package cast

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Chromecast speaks Cast v2: protobuf-framed JSON over TLS on port 8009. The frame is small
// enough to write by hand, so no library is needed.

const (
	nsConnection = "urn:x-cast:com.google.cast.tp.connection"
	nsHeartbeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
	nsReceiver   = "urn:x-cast:com.google.cast.receiver"
	nsMedia      = "urn:x-cast:com.google.cast.media"
	mediaApp     = "CC1AD845" // the Default Media Receiver
	sender       = "sender-trackferry"
)

type castMessage struct {
	Source, Dest, Namespace, Payload string
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendString(b []byte, field int, s string) []byte {
	b = appendVarint(b, uint64(field)<<3|2)
	b = appendVarint(b, uint64(len(s)))
	return append(b, s...)
}

// encode writes the CastMessage protobuf: protocol 0, the ids, the namespace, a string payload.
func (m castMessage) encode() []byte {
	b := []byte{1 << 3, 0}
	b = appendString(b, 2, m.Source)
	b = appendString(b, 3, m.Dest)
	b = appendString(b, 4, m.Namespace)
	b = append(b, 5<<3, 0)
	return appendString(b, 6, m.Payload)
}

func readVarint(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errors.New("cast: bad varint")
}

func decodeCast(b []byte) (castMessage, error) {
	var m castMessage
	for len(b) > 0 {
		key, n, err := readVarint(b)
		if err != nil {
			return m, err
		}
		b = b[n:]
		field, wire := key>>3, key&7
		switch wire {
		case 0:
			_, n, err := readVarint(b)
			if err != nil {
				return m, err
			}
			b = b[n:]
		case 2:
			l, n, err := readVarint(b)
			if err != nil || uint64(len(b)-n) < l {
				return m, errors.New("cast: bad length")
			}
			s := string(b[n : n+int(l)])
			b = b[n+int(l):]
			switch field {
			case 2:
				m.Source = s
			case 3:
				m.Dest = s
			case 4:
				m.Namespace = s
			case 6:
				m.Payload = s
			}
		default:
			return m, fmt.Errorf("cast: wire type %d", wire)
		}
	}
	return m, nil
}

func writeFrame(w io.Writer, m castMessage) error {
	body := m.encode()
	head := make([]byte, 4)
	binary.BigEndian.PutUint32(head, uint32(len(body)))
	_, err := w.Write(append(head, body...))
	return err
}

func readFrame(r io.Reader) (castMessage, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(r, head); err != nil {
		return castMessage{}, err
	}
	n := binary.BigEndian.Uint32(head)
	if n > 1<<20 {
		return castMessage{}, errors.New("cast: frame too large")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return castMessage{}, err
	}
	return decodeCast(body)
}

// castSession is one connection to a device, with the media app running on it.
type castSession struct {
	conn  io.ReadWriteCloser
	wmu   sync.Mutex
	mu    sync.Mutex
	next  int
	wait  map[int]chan map[string]any
	media map[string]any // the newest media status
	recv  map[string]any // the newest receiver status
	app   string         // transport id of the media app
	appS  string         // its session id
	err   error
	done  chan struct{}
}

func newSession(conn io.ReadWriteCloser) *castSession {
	s := &castSession{conn: conn, wait: map[int]chan map[string]any{}, done: make(chan struct{})}
	go s.read()
	go s.beat()
	return s
}

func (s *castSession) send(dest, ns string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return writeFrame(s.conn, castMessage{Source: sender, Dest: dest, Namespace: ns, Payload: string(b)})
}

// ask sends a request and waits for the answer that carries its requestId.
func (s *castSession) ask(ctx context.Context, dest, ns string, v map[string]any) (map[string]any, error) {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return nil, s.err
	}
	s.next++
	id := s.next
	ch := make(chan map[string]any, 1)
	s.wait[id] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.wait, id); s.mu.Unlock() }()
	v["requestId"] = id
	if err := s.send(dest, ns, v); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		switch r["type"] {
		case "LOAD_FAILED", "LOAD_CANCELLED", "INVALID_REQUEST", "INVALID_PLAYER_STATE", "LAUNCH_ERROR":
			return r, fmt.Errorf("设备拒绝了：%v", r["type"])
		}
		return r, nil
	case <-s.done:
		return nil, s.failure()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		return nil, errors.New("设备没有回答")
	}
}

func (s *castSession) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		return errors.New("和设备的连接断了")
	}
	return s.err
}

func (s *castSession) read() {
	defer close(s.done)
	for {
		m, err := readFrame(s.conn)
		if err != nil {
			s.mu.Lock()
			s.err = fmt.Errorf("和设备的连接断了：%w", err)
			s.mu.Unlock()
			return
		}
		var p map[string]any
		if json.Unmarshal([]byte(m.Payload), &p) != nil {
			continue
		}
		if m.Namespace == nsHeartbeat && p["type"] == "PING" {
			_ = s.send(m.Source, nsHeartbeat, map[string]any{"type": "PONG"})
			continue
		}
		s.mu.Lock()
		switch p["type"] {
		case "MEDIA_STATUS":
			if list, ok := p["status"].([]any); ok && len(list) > 0 {
				if st, ok := list[0].(map[string]any); ok {
					s.media = st
				}
			} else if ok {
				s.media = nil
			}
		case "RECEIVER_STATUS":
			if st, ok := p["status"].(map[string]any); ok {
				s.recv = st
			}
		case "CLOSE":
			if m.Source == s.app {
				s.app, s.media = "", nil
			}
		}
		if id, ok := p["requestId"].(float64); ok && id > 0 {
			if ch := s.wait[int(id)]; ch != nil {
				ch <- p
			}
		}
		s.mu.Unlock()
	}
}

func (s *castSession) beat() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			if s.send("receiver-0", nsHeartbeat, map[string]any{"type": "PING"}) != nil {
				s.conn.Close()
				return
			}
		}
	}
}

func (s *castSession) close() { s.conn.Close() }

// ensureApp starts the media app if it does not run, and connects to it.
func (s *castSession) ensureApp(ctx context.Context) (string, error) {
	s.mu.Lock()
	app := s.app
	s.mu.Unlock()
	if app != "" {
		return app, nil
	}
	find := func(st map[string]any) (string, string) {
		apps, _ := st["applications"].([]any)
		for _, a := range apps {
			m, _ := a.(map[string]any)
			if m["appId"] == mediaApp {
				t, _ := m["transportId"].(string)
				sid, _ := m["sessionId"].(string)
				return t, sid
			}
		}
		return "", ""
	}
	r, err := s.ask(ctx, "receiver-0", nsReceiver, map[string]any{"type": "GET_STATUS"})
	if err != nil {
		return "", err
	}
	st, _ := r["status"].(map[string]any)
	t, sid := find(st)
	if t == "" {
		if r, err = s.ask(ctx, "receiver-0", nsReceiver, map[string]any{"type": "LAUNCH", "appId": mediaApp}); err != nil {
			return "", err
		}
		st, _ = r["status"].(map[string]any)
		if t, sid = find(st); t == "" {
			return "", errors.New("设备没有打开播放器")
		}
	}
	if err := s.send(t, nsConnection, map[string]any{"type": "CONNECT"}); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.app, s.appS = t, sid
	s.mu.Unlock()
	return t, nil
}

// castRenderer controls a Chromecast; it connects again by itself when the link drops.
type castRenderer struct {
	addr string
	dial func(ctx context.Context, addr string) (io.ReadWriteCloser, error)

	mu  sync.Mutex
	s   *castSession
	vol int
}

func newChromecast(addr string) *castRenderer {
	return &castRenderer{addr: addr, vol: -1, dial: func(ctx context.Context, addr string) (io.ReadWriteCloser, error) {
		d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: &tls.Config{
			// Cast devices carry certificates of their own maker, not of a public authority.
			InsecureSkipVerify: true, //nolint:gosec
		}}
		return d.DialContext(ctx, "tcp", addr)
	}}
}

func (r *castRenderer) session(ctx context.Context) (*castSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.s != nil {
		select {
		case <-r.s.done:
			r.s = nil
		default:
			return r.s, nil
		}
	}
	conn, err := r.dial(ctx, r.addr)
	if err != nil {
		return nil, err
	}
	s := newSession(conn)
	if err := s.send("receiver-0", nsConnection, map[string]any{"type": "CONNECT"}); err != nil {
		s.close()
		return nil, err
	}
	r.s = s
	return s, nil
}

// media sends a media command for the current song.
func (r *castRenderer) media(ctx context.Context, kind string, extra map[string]any) error {
	s, err := r.session(ctx)
	if err != nil {
		return err
	}
	app, err := s.ensureApp(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	var id any
	if s.media != nil {
		id = s.media["mediaSessionId"]
	}
	s.mu.Unlock()
	if id == nil {
		return errors.New("设备上没有在放的歌")
	}
	v := map[string]any{"type": kind, "mediaSessionId": id}
	for k, x := range extra {
		v[k] = x
	}
	_, err = s.ask(ctx, app, nsMedia, v)
	return err
}

func (r *castRenderer) Load(ctx context.Context, m Media, start time.Duration) error {
	s, err := r.session(ctx)
	if err != nil {
		return err
	}
	app, err := s.ensureApp(ctx)
	if err != nil {
		return err
	}
	meta := map[string]any{"metadataType": 3, "title": m.Title}
	if m.Artist != "" {
		meta["artist"] = m.Artist
	}
	if m.Album != "" {
		meta["albumName"] = m.Album
	}
	media := map[string]any{"contentId": m.URL, "contentType": m.Type, "streamType": "BUFFERED", "metadata": meta}
	if m.Duration > 0 {
		media["duration"] = m.Duration.Seconds()
	}
	s.mu.Lock()
	sid := s.appS
	s.mu.Unlock()
	_, err = s.ask(ctx, app, nsMedia, map[string]any{"type": "LOAD", "sessionId": sid, "media": media, "autoplay": true, "currentTime": start.Seconds()})
	return err
}

func (r *castRenderer) Play(ctx context.Context) error  { return r.media(ctx, "PLAY", nil) }
func (r *castRenderer) Pause(ctx context.Context) error { return r.media(ctx, "PAUSE", nil) }
func (r *castRenderer) Stop(ctx context.Context) error  { return r.media(ctx, "STOP", nil) }
func (r *castRenderer) Seek(ctx context.Context, to time.Duration) error {
	return r.media(ctx, "SEEK", map[string]any{"currentTime": to.Seconds()})
}

func (r *castRenderer) SetVolume(ctx context.Context, v int) error {
	s, err := r.session(ctx)
	if err != nil {
		return err
	}
	_, err = s.ask(ctx, "receiver-0", nsReceiver, map[string]any{"type": "SET_VOLUME", "volume": map[string]any{"level": float64(v) / 100}})
	return err
}

func (r *castRenderer) Status(ctx context.Context) (Status, error) {
	s, err := r.session(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{State: Stopped, Volume: -1}
	if rs, err := s.ask(ctx, "receiver-0", nsReceiver, map[string]any{"type": "GET_STATUS"}); err == nil {
		if recv, ok := rs["status"].(map[string]any); ok {
			if v, ok := recv["volume"].(map[string]any); ok {
				if l, ok := v["level"].(float64); ok {
					st.Volume = int(l*100 + .5)
				}
			}
		}
	} else {
		return Status{}, err
	}
	s.mu.Lock()
	app := s.app
	s.mu.Unlock()
	if app == "" {
		return st, nil // the media app is not open: nothing plays
	}
	if _, err := s.ask(ctx, app, nsMedia, map[string]any{"type": "GET_STATUS"}); err != nil {
		return st, err
	}
	s.mu.Lock()
	m := s.media
	s.mu.Unlock()
	if m == nil {
		return st, nil
	}
	switch m["playerState"] {
	case "PLAYING":
		st.State = Playing
	case "PAUSED":
		st.State = Paused
	case "BUFFERING", "LOADING":
		st.State = Buffering
	default:
		switch m["idleReason"] {
		case "FINISHED":
			st.State = Finished
		case "ERROR":
			st.State = Failed
		}
	}
	if t, ok := m["currentTime"].(float64); ok {
		st.Position = time.Duration(t * float64(time.Second))
	}
	if med, ok := m["media"].(map[string]any); ok {
		if d, ok := med["duration"].(float64); ok {
			st.Duration = time.Duration(d * float64(time.Second))
		}
		st.URL, _ = med["contentId"].(string)
	}
	return st, nil
}

func (r *castRenderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.s != nil {
		r.s.close()
		r.s = nil
	}
}

// castID is the device's own id from its announcement, else its address.
func castID(txt []string, addr string) string {
	for _, t := range txt {
		if v, ok := strings.CutPrefix(t, "id="); ok && v != "" {
			return safeID("cast-", v)
		}
	}
	return safeID("cast-", addr)
}
