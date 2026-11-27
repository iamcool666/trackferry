package cast

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"trackferry/internal/tv"
)

// Prepare turns a song of a queue into an address the device can fetch.
type Prepare func(ctx context.Context, ref tv.Ref) (Media, error)

// How the player paces itself. Tests shorten them.
var (
	tickEvery  = time.Second
	startFor   = 15 * time.Second // a song must start playing within this
	statusFor  = 4 * time.Second
	commandFor = 20 * time.Second
	lostAfter  = 3 // failed status reads in a row before the device counts as gone
)

type job struct {
	cmd  *tv.Command // nil: the song is over, go on by the mode
	auto bool
}

// player drives one device and reports it to the TV remote as if it were a TrackFerry TV.
type player struct {
	id      string
	name    string
	r       Renderer
	remote  *tv.Remote
	prepare Prepare
	log     *slog.Logger

	jobs chan job

	mu         sync.Mutex
	queue      []tv.Ref
	index      int
	mode       string
	revision   string
	entry      string
	entries    int
	media      Media
	last       Status
	loading    bool
	loadedAt   time.Time
	sawPlaying bool // the current song has been heard playing
	held       bool // paused or stopped on purpose, or another app took the device
	advancing  bool // an end of song is being handled
	away       bool // another app took the device; our song waits at awayAt
	awayAt     time.Duration
	result     *tv.Result
	taken      map[string]bool // commands already taken up
	lost       int
}

func newPlayer(id, name string, r Renderer, remote *tv.Remote, prepare Prepare, log *slog.Logger) *player {
	return &player{id: id, name: name, r: r, remote: remote, prepare: prepare, log: log, mode: "loop",
		jobs: make(chan job, 4), taken: map[string]bool{}, revision: token()}
}

func token() string {
	b := make([]byte, 6)
	crand.Read(b)
	return hex.EncodeToString(b)
}

func (p *player) run(ctx context.Context) {
	defer p.r.Close()
	go p.work(ctx)
	epoch, seq := token(), int64(0)
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		sctx, cancel := context.WithTimeout(ctx, statusFor)
		st, err := p.r.Status(sctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		if err != nil {
			p.lost++
			if p.lost == lostAfter {
				p.log.Warn("投放　设备没有回应", "设备", p.name, "原因", err.Error())
				if p.remote != nil {
					p.remote.Gone(p.id) // offline on every screen now, not when the last report grows old
				}
			}
		} else {
			if p.lost >= lostAfter {
				p.log.Info("投放　设备回来了", "设备", p.name)
			}
			p.lost = 0
			p.observe(st)
		}
		// A device that does not answer stops reporting, so every screen shows it offline.
		report := p.lost < lostAfter
		pb := p.playback()
		res := p.result
		p.mu.Unlock()
		if report {
			seq++
			u := p.remote.Sync(p.id, tv.Heartbeat{Epoch: epoch, Sequence: seq, Playback: pb, Result: res})
			p.mu.Lock()
			if u.ResultAccepted && p.result == res {
				p.result = nil
			}
			if c := u.Command; c != nil && !p.taken[c.ID] {
				if len(p.taken) > 256 {
					p.taken = map[string]bool{}
				}
				p.taken[c.ID] = true
				cmd := *c
				select {
				case p.jobs <- job{cmd: &cmd}:
				default:
					p.result = &tv.Result{ID: cmd.ID, State: "failed", Message: "设备正忙，请稍后再试"}
				}
			}
			p.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// observe takes in what the device says; called with mu held.
func (p *player) observe(st Status) {
	before := p.last.Position
	p.last = st
	if p.loading || p.entry == "" || p.held || p.advancing {
		return
	}
	if st.State == Playing || st.State == Paused {
		if st.URL != "" && p.media.URL != "" && st.URL != p.media.URL && p.sawPlaying {
			// Someone else put something on: leave the device to them.
			p.held, p.away, p.awayAt = true, true, before
			p.log.Info("投放　设备被别的应用占用，不再接着放", "设备", p.name)
			return
		}
		if st.State == Playing {
			p.sawPlaying = true
		}
		return
	}
	over := st.State == Finished || p.sawPlaying && st.State == Stopped
	if !over && st.State == Failed {
		over = true
		p.log.Warn("投放　设备放不了这首，跳到下一首", "设备", p.name)
	}
	if !over && !p.sawPlaying && st.State == Stopped && time.Since(p.loadedAt) > startFor {
		over = true // it never started
		p.log.Warn("投放　设备一直没开始放，跳到下一首", "设备", p.name)
	}
	if over {
		p.advancing = true
		select {
		case p.jobs <- job{auto: true}:
		default:
			p.advancing = false
		}
	}
}

// playback is the TV report; called with mu held.
func (p *player) playback() tv.Playback {
	pb := tv.Playback{Revision: p.revision, Entry: p.entry, Count: len(p.queue), Mode: p.mode, Queue: append([]tv.Ref{}, p.queue...)}
	if v := p.last.Volume; v >= 0 {
		pb.Volume = &v
	}
	if len(p.queue) == 0 {
		pb.Phase, pb.Queue = "idle", []tv.Ref{}
		return pb
	}
	pb.Index, pb.Current = p.index, p.queue[p.index]
	pb.Position, pb.Duration = p.last.Position.Milliseconds(), p.last.Duration.Milliseconds()
	if pb.Duration <= 0 {
		pb.Duration = p.media.Duration.Milliseconds()
	}
	if pb.Duration > 0 && pb.Position > pb.Duration {
		pb.Position = pb.Duration
	}
	pb.Seekable = p.entry != "" && !p.loading
	if p.away {
		// What plays now is not ours: show our song where it was left.
		pb.Phase, pb.Position, pb.Duration, pb.Seekable = "paused", p.awayAt.Milliseconds(), p.media.Duration.Milliseconds(), false
		return pb
	}
	switch {
	case p.loading || p.advancing:
		pb.Phase = "buffering"
	case p.last.State == Playing:
		pb.Phase = "playing"
	case p.last.State == Buffering:
		pb.Phase = "buffering"
	case p.last.State == Failed:
		pb.Phase = "error"
	case p.last.State == Stopped && !p.sawPlaying && !p.held && p.entry != "" && time.Since(p.loadedAt) < startFor:
		pb.Phase = "buffering"
	default:
		pb.Phase = "paused"
	}
	if p.entry == "" {
		pb.Phase, pb.Position = "paused", 0
	}
	return pb
}

func (p *player) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-p.jobs:
			jctx, cancel := context.WithTimeout(ctx, commandFor)
			if j.cmd == nil {
				p.ended(jctx)
			} else {
				err := p.do(jctx, *j.cmd)
				r := tv.Result{ID: j.cmd.ID, State: "done"}
				if err != nil {
					r.State, r.Message = "failed", err.Error()
					p.log.Info("投放　操作没完成", "设备", p.name, "动作", j.cmd.Action, "原因", err.Error())
				}
				p.mu.Lock()
				p.result = &r
				p.mu.Unlock()
			}
			cancel()
		}
	}
}

// ended goes on to the next song after one is over, skipping songs that cannot be played.
func (p *player) ended(ctx context.Context) {
	defer func() { p.mu.Lock(); p.advancing = false; p.mu.Unlock() }()
	p.mu.Lock()
	n, from := len(p.queue), p.index
	p.mu.Unlock()
	if n == 0 {
		return
	}
	at := from
	for try := 0; try < min(n, 3); try++ {
		at = p.after(at, true)
		if err := p.start(ctx, at, 0); err == nil {
			return
		} else if ctx.Err() != nil {
			return
		} else {
			p.log.Warn("投放　这首放不了，换下一首", "设备", p.name, "原因", err.Error())
		}
	}
	p.mu.Lock()
	p.held = true
	p.mu.Unlock()
}

// after is the song after i by the mode; auto: the last one ended by itself.
func (p *player) after(i int, auto bool) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.queue)
	switch {
	case n <= 1:
		return 0
	case p.mode == "one" && auto:
		return i
	case p.mode == "shuffle":
		j := rand.IntN(n - 1)
		if j >= i {
			j++
		}
		return j
	}
	return (i + 1) % n
}

// start plays song i of the queue from pos and waits until the device plays it.
func (p *player) start(ctx context.Context, i int, pos time.Duration) error {
	p.mu.Lock()
	if i < 0 || i >= len(p.queue) {
		p.mu.Unlock()
		return errors.New("曲目不在队列里")
	}
	ref := p.queue[i]
	p.index, p.loading, p.held, p.sawPlaying, p.away = i, true, false, false, false
	p.entries++
	p.entry = p.revision + "-" + strconv.Itoa(p.entries)
	p.last = Status{State: Buffering, Position: pos, Volume: p.last.Volume}
	p.mu.Unlock()
	done := func(m Media) {
		p.mu.Lock()
		p.media, p.loading, p.loadedAt = m, false, time.Now()
		p.mu.Unlock()
	}
	m, err := p.prepare(ctx, ref)
	if err != nil {
		done(Media{})
		return err
	}
	if err := p.r.Load(ctx, m, pos); err != nil {
		done(m)
		return err
	}
	done(m)
	// A device takes the address at once and fetches it after; it has started only once it plays.
	deadline := time.Now().Add(startFor)
	for time.Now().Before(deadline) {
		st, err := p.r.Status(ctx)
		if err == nil && st.State == Playing {
			p.mu.Lock()
			p.sawPlaying = true
			p.mu.Unlock()
			return nil
		}
		if err == nil && st.State == Failed {
			return errors.New("设备放不了这首歌")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tickEvery / 2):
		}
	}
	return errors.New("设备没有开始播放")
}

func (p *player) do(ctx context.Context, c tv.Command) error {
	p.mu.Lock()
	n, index, state := len(p.queue), p.index, p.last.State
	p.mu.Unlock()
	switch c.Action {
	case "load":
		if c.Index < 0 || c.Index >= len(c.Items) {
			return errors.New("选中的曲目不在队列里")
		}
		p.mu.Lock()
		p.queue, p.revision = append([]tv.Ref{}, c.Items...), token()
		p.mode = c.Mode
		if p.mode == "" {
			p.mode = "loop"
		}
		p.mu.Unlock()
		return p.start(ctx, c.Index, time.Duration(c.Position)*time.Millisecond)
	case "insert":
		if len(c.Items) != 1 {
			return errors.New("每次只加一首")
		}
		if n == 0 {
			p.mu.Lock()
			p.queue, p.revision = append([]tv.Ref{}, c.Items...), token()
			p.mu.Unlock()
			return p.start(ctx, 0, 0)
		}
		p.mu.Lock()
		at := len(p.queue)
		if c.QueueMode == "next" {
			at = p.index + 1
		}
		if len(p.queue) >= tv.MaxQueue {
			p.mu.Unlock()
			return errors.New("播放队列已满")
		}
		p.queue = append(p.queue[:at], append([]tv.Ref{c.Items[0]}, p.queue[at:]...)...)
		p.revision = token()
		p.mu.Unlock()
		return nil
	case "play":
		if n == 0 {
			return errors.New("没有待播曲目")
		}
		p.mu.Lock()
		// Stopped or over: start the song again; paused: go on.
		fresh := p.entry == "" || state == Finished || state == Stopped
		away, at := p.away, p.awayAt
		p.held = false
		p.mu.Unlock()
		if away {
			return p.start(ctx, index, at) // take the device back, where our song was left
		}
		if fresh {
			return p.start(ctx, index, 0)
		}
		return p.r.Play(ctx)
	case "pause", "stop":
		p.mu.Lock()
		p.held = true
		away := p.away
		p.mu.Unlock()
		if away {
			return nil // what plays is another app's: leave it alone
		}
		if c.Action == "stop" {
			return p.r.Stop(ctx)
		}
		return p.r.Pause(ctx)
	case "next":
		if n == 0 {
			return errors.New("没有下一首")
		}
		return p.start(ctx, p.after(index, false), 0)
	case "previous":
		if n == 0 {
			return errors.New("没有上一首")
		}
		p.mu.Lock()
		pos := p.last.Position
		p.mu.Unlock()
		if pos > 3*time.Second {
			return p.start(ctx, index, 0)
		}
		return p.start(ctx, (index-1+n)%n, 0)
	case "jump":
		return p.start(ctx, c.Index, 0)
	case "remove":
		if c.Index < 0 || c.Index >= n || c.Index == index {
			return errors.New("不能移除当前曲目")
		}
		p.mu.Lock()
		p.queue = append(p.queue[:c.Index:c.Index], p.queue[c.Index+1:]...)
		if c.Index < p.index {
			p.index--
		}
		p.revision = token()
		p.mu.Unlock()
		return nil
	case "mode":
		p.mu.Lock()
		p.mode = c.Mode
		p.mu.Unlock()
		return nil
	case "volume":
		if c.Volume == nil {
			return errors.New("没有音量")
		}
		return p.r.SetVolume(ctx, *c.Volume)
	case "seek":
		if n == 0 {
			return errors.New("没有在放的歌")
		}
		to := time.Duration(c.Position) * time.Millisecond
		p.mu.Lock()
		if p.away {
			p.awayAt = to // where our song starts when it is played again
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		if err := p.r.Seek(ctx, to); err != nil {
			return err
		}
		p.mu.Lock()
		p.last.Position = to
		p.mu.Unlock()
		return nil
	}
	return ErrUnsupported
}
