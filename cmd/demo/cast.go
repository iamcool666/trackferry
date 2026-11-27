package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	panelAt    = "127.0.0.1:18590"
	dlnaPort   = 18591
	castPort   = 18592
	dlnaUDN    = "uuid:trackferry-demo-dlna-0001"
	dlnaName   = "客厅音箱（模拟 DLNA）"
	castName   = "卧室 Chromecast（模拟）"
	castDevice = "trackferrydemocast0001"
)

// deck is a pretend player: it fetches the song to see it is there, then keeps time.
type deck struct {
	name string
	mu   sync.Mutex
	url  string
	meta string // title from the sender
	dur  time.Duration
	// "idle", "loading", "playing", "paused", "finished", "error"
	state   string
	base    time.Duration
	since   time.Time
	vol     int
	off     time.Time // offline until
	fetched int64
	seq     int
}

func (d *deck) pos() time.Duration {
	p := d.base
	if d.state == "playing" {
		p += time.Since(d.since)
	}
	if d.dur > 0 && p >= d.dur {
		p = d.dur
	}
	return p
}

// tick turns a song that ran out into a finished one.
func (d *deck) tick() {
	if d.state == "playing" && d.dur > 0 && d.pos() >= d.dur {
		d.base, d.state = d.dur, "finished"
		log.Printf("%s　放完：%s", d.name, d.meta)
	}
}

func (d *deck) load(u, title string, dur, start time.Duration) {
	d.seq++
	seq := d.seq
	d.url, d.meta, d.dur, d.state, d.base, d.fetched = u, title, dur, "loading", start, 0
	log.Printf("%s　载入：%s  %s", d.name, title, u)
	go func() {
		got, err := fetch(u)
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.seq != seq {
			return
		}
		d.fetched = got
		if err != nil {
			d.state = "error"
			log.Printf("%s　取歌失败：%v", d.name, err)
			return
		}
		d.state, d.since = "playing", time.Now()
		log.Printf("%s　开始放（取到 %d KB）", d.name, got/1024)
	}()
}

func (d *deck) play() {
	if d.state == "paused" {
		d.state, d.since = "playing", time.Now()
	} else if d.state == "finished" || d.state == "idle" && d.url != "" {
		d.base, d.state, d.since = 0, "playing", time.Now()
	}
}

func (d *deck) pause() {
	if d.state == "playing" {
		d.base, d.state = d.pos(), "paused"
	}
}

func (d *deck) seek(to time.Duration) {
	d.base, d.since = max(0, to), time.Now()
}

func (d *deck) offline() bool { return time.Now().Before(d.off) }

// fetch reads the start of the song the way a device would.
func fetch(u string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-")
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return io.Copy(io.Discard, io.LimitReader(res.Body, 512<<10))
}

func lanIP() (net.IP, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
				return n.IP.To4(), nil
			}
		}
	}
	return nil, errors.New("没找到局域网地址")
}

func runCast() error {
	ip, err := lanIP()
	if err != nil {
		return err
	}
	dl := &deck{name: "DLNA", state: "idle", vol: 30}
	cc := &deck{name: "Chromecast", state: "idle", vol: 40}
	go func() {
		for range time.Tick(200 * time.Millisecond) {
			for _, d := range []*deck{dl, cc} {
				d.mu.Lock()
				d.tick()
				d.mu.Unlock()
			}
		}
	}()
	go func() { log.Fatal(serveDLNA(ip, dl)) }()
	go func() { log.Fatal(serveSSDP(ip, dl)) }()
	go func() { log.Fatal(serveCast(cc)) }()
	srv, err := zeroconf.Register("TrackFerry-Demo-Cast", "_googlecast._tcp", "local.", castPort,
		[]string{"id=" + castDevice, "fn=" + castName, "md=Chromecast Audio（模拟）", "ve=05", "rs="}, nil)
	if err != nil {
		return fmt.Errorf("广播 Chromecast：%w", err)
	}
	defer srv.Shutdown()
	log.Printf("模拟设备已就绪：DLNA http://%s:%d/desc.xml，Chromecast %s:%d；控制页 http://%s", ip, dlnaPort, ip, castPort, panelAt)
	return servePanel(dl, cc)
}

// --- DLNA ---

func serveSSDP(ip net.IP, d *deck) error {
	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return fmt.Errorf("SSDP：%w", err)
	}
	reply, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		msg := string(buf[:n])
		if !strings.HasPrefix(msg, "M-SEARCH") {
			continue
		}
		st := ""
		for _, l := range strings.Split(msg, "\r\n") {
			if k, v, ok := strings.Cut(l, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "ST") {
				st = strings.TrimSpace(v)
			}
		}
		if st != "ssdp:all" && !strings.Contains(st, "MediaRenderer") && !strings.Contains(st, "AVTransport") {
			continue
		}
		d.mu.Lock()
		off := d.offline()
		d.mu.Unlock()
		if off {
			continue
		}
		out := "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nEXT:\r\nLOCATION: http://" + ip.String() + ":" + strconv.Itoa(dlnaPort) + "/desc.xml\r\nSERVER: Demo/1.0 UPnP/1.0\r\nST: " + st + "\r\nUSN: " + dlnaUDN + "::" + st + "\r\n\r\n"
		_, _ = reply.WriteToUDP([]byte(out), from)
	}
}

const description = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion>
<device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>%s</friendlyName>
<manufacturer>TrackFerry Demo</manufacturer><modelName>模拟音箱</modelName><UDN>%s</UDN>
<serviceList>
<service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><serviceId>urn:upnp-org:serviceId:AVTransport</serviceId><controlURL>/ctl/av</controlURL><eventSubURL>/evt/av</eventSubURL><SCPDURL>/av.xml</SCPDURL></service>
<service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><serviceId>urn:upnp-org:serviceId:RenderingControl</serviceId><controlURL>/ctl/rc</controlURL><eventSubURL>/evt/rc</eventSubURL><SCPDURL>/rc.xml</SCPDURL></service>
</serviceList></device></root>`

var durationAttr = regexp.MustCompile(`duration="([0-9:.]+)"`)

func clockOf(s string) time.Duration {
	s, _, _ = strings.Cut(s, ".")
	var h, m, sec int
	if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err != nil {
		return 0
	}
	return time.Duration(h*3600+m*60+sec) * time.Second
}

func clockText(d time.Duration) string {
	s := int(d / time.Second)
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
}

// soapArgs reads the arguments of a SOAP request by their names.
func soapArgs(r io.Reader) (string, map[string]string) {
	dec := xml.NewDecoder(io.LimitReader(r, 1<<20))
	args := map[string]string{}
	var action, cur string
	depth := 0
	for {
		t, err := dec.Token()
		if err != nil {
			return action, args
		}
		switch v := t.(type) {
		case xml.StartElement:
			depth++
			if depth == 3 {
				action = v.Name.Local
			}
			cur = v.Name.Local
		case xml.CharData:
			if depth == 4 {
				args[cur] += string(v)
			}
		case xml.EndElement:
			depth--
		}
	}
}

func serveDLNA(ip net.IP, d *deck) error {
	mux := http.NewServeMux()
	drop := func(w http.ResponseWriter) bool {
		d.mu.Lock()
		off := d.offline()
		d.mu.Unlock()
		if off {
			if hj, ok := w.(http.Hijacker); ok {
				if c, _, err := hj.Hijack(); err == nil {
					c.Close()
				}
			}
		}
		return off
	}
	mux.HandleFunc("GET /desc.xml", func(w http.ResponseWriter, r *http.Request) {
		if drop(w) {
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		fmt.Fprintf(w, description, dlnaName, dlnaUDN)
	})
	answer := func(w http.ResponseWriter, service, action string, out [][2]string) {
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
		b.WriteString(`<u:` + action + `Response xmlns:u="` + service + `">`)
		for _, a := range out {
			b.WriteString("<" + a[0] + ">" + html.EscapeString(a[1]) + "</" + a[0] + ">")
		}
		b.WriteString(`</u:` + action + `Response></s:Body></s:Envelope>`)
		io.WriteString(w, b.String())
	}
	fault := func(w http.ResponseWriter, code int, text string) {
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, code, text)
	}
	const av, rc = "urn:schemas-upnp-org:service:AVTransport:1", "urn:schemas-upnp-org:service:RenderingControl:1"
	mux.HandleFunc("POST /ctl/av", func(w http.ResponseWriter, r *http.Request) {
		if drop(w) {
			return
		}
		action, args := soapArgs(r.Body)
		d.mu.Lock()
		defer d.mu.Unlock()
		switch action {
		case "SetAVTransportURI":
			title := "?"
			var item struct {
				Title string `xml:"item>title"`
			}
			if xml.Unmarshal([]byte(args["CurrentURIMetaData"]), &item) == nil && item.Title != "" {
				title = item.Title
			}
			var dur time.Duration
			if m := durationAttr.FindStringSubmatch(args["CurrentURIMetaData"]); m != nil {
				dur = clockOf(m[1])
			}
			d.url, d.meta, d.dur, d.state, d.base = strings.TrimSpace(args["CurrentURI"]), title, dur, "idle", 0
			d.seq++
		case "Play":
			if d.url == "" {
				fault(w, 701, "Transition not available")
				return
			}
			if d.state == "idle" {
				d.load(d.url, d.meta, d.dur, d.base)
			} else {
				d.play()
			}
		case "Pause":
			d.pause()
		case "Stop":
			d.seq++
			d.state, d.base = "idle", 0
		case "Seek":
			d.seek(clockOf(args["Target"]))
		case "GetTransportInfo":
			state, status := "STOPPED", "OK"
			switch d.state {
			case "playing":
				state = "PLAYING"
			case "paused":
				state = "PAUSED_PLAYBACK"
			case "loading":
				state = "TRANSITIONING"
			case "error":
				status = "ERROR_OCCURRED"
			}
			if d.url == "" {
				state = "NO_MEDIA_PRESENT"
			}
			answer(w, av, action, [][2]string{{"CurrentTransportState", state}, {"CurrentTransportStatus", status}, {"CurrentSpeed", "1"}})
			return
		case "GetPositionInfo":
			pos := d.pos()
			if d.state == "idle" || d.state == "finished" {
				pos = 0
			}
			answer(w, av, action, [][2]string{{"Track", "1"}, {"TrackDuration", clockText(d.dur)}, {"TrackURI", d.url}, {"RelTime", clockText(pos)}, {"AbsTime", clockText(pos)}})
			return
		default:
			fault(w, 401, "Invalid Action")
			return
		}
		if d.state == "finished" {
			// A real renderer goes back to STOPPED when the song ends.
			d.state = "idle"
		}
		answer(w, av, action, nil)
	})
	mux.HandleFunc("POST /ctl/rc", func(w http.ResponseWriter, r *http.Request) {
		if drop(w) {
			return
		}
		action, args := soapArgs(r.Body)
		d.mu.Lock()
		defer d.mu.Unlock()
		switch action {
		case "GetVolume":
			answer(w, rc, action, [][2]string{{"CurrentVolume", strconv.Itoa(d.vol)}})
		case "SetVolume":
			if v, err := strconv.Atoi(args["DesiredVolume"]); err == nil && v >= 0 && v <= 100 {
				d.vol = v
			}
			answer(w, rc, action, nil)
		default:
			fault(w, 401, "Invalid Action")
		}
	})
	return (&http.Server{Addr: net.JoinHostPort(ip.String(), strconv.Itoa(dlnaPort)), Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe()
}

// --- Chromecast ---

type frame struct{ src, dst, ns, payload string }

func putVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func writeCast(w io.Writer, f frame) error {
	str := func(b []byte, field int, s string) []byte {
		b = putVarint(b, uint64(field)<<3|2)
		b = putVarint(b, uint64(len(s)))
		return append(b, s...)
	}
	b := []byte{1 << 3, 0}
	b = str(b, 2, f.src)
	b = str(b, 3, f.dst)
	b = str(b, 4, f.ns)
	b = append(b, 5<<3, 0)
	b = str(b, 6, f.payload)
	head := binary.BigEndian.AppendUint32(nil, uint32(len(b)))
	_, err := w.Write(append(head, b...))
	return err
}

func readCast(r io.Reader) (frame, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return frame{}, err
	}
	n := binary.BigEndian.Uint32(head[:])
	if n > 1<<20 {
		return frame{}, errors.New("too large")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return frame{}, err
	}
	var f frame
	for len(b) > 0 {
		key, k := binary.Uvarint(b)
		if k <= 0 {
			return f, errors.New("bad varint")
		}
		b = b[k:]
		switch key & 7 {
		case 0:
			_, k = binary.Uvarint(b)
			if k <= 0 {
				return f, errors.New("bad varint")
			}
			b = b[k:]
		case 2:
			l, k := binary.Uvarint(b)
			if k <= 0 || uint64(len(b)-k) < l {
				return f, errors.New("bad length")
			}
			s := string(b[k : k+int(l)])
			b = b[k+int(l):]
			switch key >> 3 {
			case 2:
				f.src = s
			case 3:
				f.dst = s
			case 4:
				f.ns = s
			case 6:
				f.payload = s
			}
		default:
			return f, errors.New("bad wire type")
		}
	}
	return f, nil
}

type castState struct {
	d        *deck
	running  bool // the media app
	mu       sync.Mutex
	conns    map[net.Conn]bool
	stranger bool // another app put something on
}

func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

var castNow *castState

func serveCast(d *deck) error {
	cert, err := selfSigned()
	if err != nil {
		return err
	}
	ln, err := tls.Listen("tcp", ":"+strconv.Itoa(castPort), &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		return fmt.Errorf("Chromecast：%w", err)
	}
	cs := &castState{d: d, conns: map[net.Conn]bool{}}
	castNow = cs
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		d.mu.Lock()
		off := d.offline()
		d.mu.Unlock()
		if off {
			c.Close()
			continue
		}
		go cs.serve(c)
	}
}

// dropAll cuts every connection, as a device that lost the network.
func (cs *castState) dropAll() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for c := range cs.conns {
		c.Close()
	}
}

func (cs *castState) serve(c net.Conn) {
	cs.mu.Lock()
	cs.conns[c] = true
	cs.mu.Unlock()
	defer func() {
		cs.mu.Lock()
		delete(cs.conns, c)
		cs.mu.Unlock()
		c.Close()
	}()
	var wmu sync.Mutex
	send := func(src, dst, ns string, v map[string]any) {
		b, _ := json.Marshal(v)
		wmu.Lock()
		defer wmu.Unlock()
		_ = writeCast(c, frame{src, dst, ns, string(b)})
	}
	const (
		nsBeat  = "urn:x-cast:com.google.cast.tp.heartbeat"
		nsRecv  = "urn:x-cast:com.google.cast.receiver"
		nsMedia = "urn:x-cast:com.google.cast.media"
		app     = "CC1AD845"
		tid     = "web-7"
	)
	for {
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
		f, err := readCast(c)
		if err != nil {
			return
		}
		var p map[string]any
		if json.Unmarshal([]byte(f.payload), &p) != nil {
			continue
		}
		id := p["requestId"]
		d := cs.d
		receiver := func() map[string]any {
			st := map[string]any{"volume": map[string]any{"level": float64(d.vol) / 100, "muted": false}}
			if cs.running {
				st["applications"] = []any{map[string]any{"appId": app, "displayName": "Default Media Receiver", "transportId": tid, "sessionId": "demo-session"}}
			}
			return map[string]any{"type": "RECEIVER_STATUS", "requestId": id, "status": st}
		}
		media := func() map[string]any {
			if d.url == "" {
				return map[string]any{"type": "MEDIA_STATUS", "requestId": id, "status": []any{}}
			}
			ps, idle := "IDLE", ""
			switch d.state {
			case "playing":
				ps = "PLAYING"
			case "paused":
				ps = "PAUSED"
			case "loading":
				ps = "BUFFERING"
			case "finished":
				idle = "FINISHED"
			case "error":
				idle = "ERROR"
			case "idle":
				idle = "CANCELLED"
			}
			st := map[string]any{"mediaSessionId": d.seq, "playerState": ps, "currentTime": d.pos().Seconds(),
				"media": map[string]any{"contentId": d.url, "duration": d.dur.Seconds()}}
			if idle != "" {
				st["idleReason"] = idle
			}
			return map[string]any{"type": "MEDIA_STATUS", "requestId": id, "status": []any{st}}
		}
		switch f.ns {
		case nsBeat:
			if p["type"] == "PING" {
				send(f.dst, f.src, nsBeat, map[string]any{"type": "PONG"})
			}
		case nsRecv:
			d.mu.Lock()
			switch p["type"] {
			case "LAUNCH":
				cs.running = true
			case "SET_VOLUME":
				if v, ok := p["volume"].(map[string]any); ok {
					if l, ok := v["level"].(float64); ok {
						d.vol = int(l*100 + .5)
					}
				}
			}
			out := receiver()
			d.mu.Unlock()
			send("receiver-0", f.src, nsRecv, out)
		case nsMedia:
			d.mu.Lock()
			switch p["type"] {
			case "LOAD":
				m, _ := p["media"].(map[string]any)
				u, _ := m["contentId"].(string)
				dur, _ := m["duration"].(float64)
				start, _ := p["currentTime"].(float64)
				title := "?"
				if meta, ok := m["metadata"].(map[string]any); ok {
					if t, ok := meta["title"].(string); ok {
						title = t
					}
				}
				d.load(u, title, time.Duration(dur*float64(time.Second)), time.Duration(start*float64(time.Second)))
			case "PLAY":
				d.play()
			case "PAUSE":
				d.pause()
			case "STOP":
				d.seq++
				d.state, d.base = "idle", 0
			case "SEEK":
				if t, ok := p["currentTime"].(float64); ok {
					d.seek(time.Duration(t * float64(time.Second)))
				}
			}
			out := media()
			d.mu.Unlock()
			send(tid, f.src, nsMedia, out)
		}
	}
}

// --- the panel ---

func servePanel(dl, cc *deck) error {
	mux := http.NewServeMux()
	row := func(d *deck, label, key string) string {
		d.mu.Lock()
		defer d.mu.Unlock()
		state := map[string]string{"idle": "停止", "loading": "取歌中", "playing": "播放中", "paused": "暂停", "finished": "放完", "error": "出错"}[d.state]
		if d.offline() {
			state = fmt.Sprintf("断线中（还有 %d 秒）", int(time.Until(d.off).Seconds())+1)
		}
		pos := d.pos()
		return fmt.Sprintf(`<section><h2>%s</h2><p><b>%s</b>　%s</p><p>%s / %s　音量 %d　已取 %d KB</p><p class=u>%s</p>
<form method=post action="/off?d=%s"><button>断线 30 秒</button></form>
<form method=post action="/other?d=%s"><button>被别的应用占用</button></form>
<form method=post action="/broken?d=%s"><button>这首放不了</button></form></section>`,
			label, html.EscapeString(state), html.EscapeString(d.meta), clockText(pos), clockText(d.dur), d.vol, d.fetched/1024, html.EscapeString(d.url), key, key, key)
	}
	pick := func(r *http.Request) *deck {
		if r.URL.Query().Get("d") == "cc" {
			return cc
		}
		return dl
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><meta http-equiv=refresh content=1><title>模拟投放设备</title>
<style>body{font:15px system-ui;margin:24px;max-width:720px;background:#111;color:#eee}section{border:1px solid #333;border-radius:10px;padding:12px 16px;margin:12px 0}form{display:inline}button{margin-right:8px;padding:6px 10px}.u{color:#888;font-size:12px;word-break:break-all}</style>
<h1>模拟投放设备</h1>%s%s`, row(dl, dlnaName, "dl"), row(cc, castName, "cc"))
	})
	back := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusSeeOther) }
	mux.HandleFunc("POST /off", func(w http.ResponseWriter, r *http.Request) {
		d := pick(r)
		d.mu.Lock()
		d.off = time.Now().Add(30 * time.Second)
		d.mu.Unlock()
		if d == cc && castNow != nil {
			castNow.dropAll()
		}
		log.Printf("%s　模拟断线 30 秒", d.name)
		back(w, r)
	})
	mux.HandleFunc("POST /other", func(w http.ResponseWriter, r *http.Request) {
		d := pick(r)
		d.mu.Lock()
		d.seq++
		d.url, d.meta, d.dur, d.state, d.base, d.since = "http://other-app.invalid/radio.mp3", "别的应用在放的电台", 0, "playing", 0, time.Now()
		d.mu.Unlock()
		log.Printf("%s　模拟被别的应用占用", d.name)
		back(w, r)
	})
	mux.HandleFunc("POST /broken", func(w http.ResponseWriter, r *http.Request) {
		d := pick(r)
		d.mu.Lock()
		if d.url != "" {
			d.seq++
			d.state = "error"
		}
		d.mu.Unlock()
		log.Printf("%s　模拟这首放不了", d.name)
		back(w, r)
	})
	return (&http.Server{Addr: panelAt, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe()
}
