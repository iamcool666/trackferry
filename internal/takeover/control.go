package takeover

import (
	"context"
	"errors"
	"time"

	"trackferry/internal/logx"
)

// Control actions.
const (
	ControlPlay   = "play"
	ControlPause  = "pause"
	ControlStop   = "stop"
	ControlVolume = "volume"
	ControlWake   = "wake"

	ControlNext     = "next"
	ControlPrevious = "previous"
	ControlMode     = "mode"
	ControlSeek     = "seek"
)

// pressed is how long a speaker gets to do what a button asked before it is looked at.
const pressed = 350 * time.Millisecond

// PauseRun pauses the source of a handoff while holding off replacement runs on that speaker.
func (e *Engine) PauseRun(ctx context.Context, dev, hardware string, id int64) error {
	d := e.device(dev)
	d.control.Lock()
	defer d.control.Unlock()

	e.mu.Lock()
	current, gen := id > 0 && d.cancel != nil && d.runID == id && d.watching, d.gen
	e.mu.Unlock()
	if !current {
		return ErrSeekMoved
	}
	p, err := e.sp.PlayStatus(ctx, dev)
	if err != nil {
		return err
	}
	if foreign(hardware, p) {
		return ErrSeekMoved
	}
	if !p.Playing() {
		return nil
	}
	if err := e.pause(ctx, dev, d, gen, true); err != nil {
		return err
	}
	p, err = e.sp.PlayStatus(ctx, dev)
	if err == nil && p.Playing() {
		err = errors.New("小爱仍在播放")
	}
	return err
}

// Press does what a button asks for, and makes sure it was done: speakers confirm a pause
// they then ignore.
func (e *Engine) Press(ctx context.Context, dev, action string) error {
	d := e.device(dev)
	e.mu.Lock()
	on, watching, cut, gen := d.cancel != nil, d.watching, d.cut, d.gen
	e.mu.Unlock()
	switch {
	case action == ControlPlay && on && watching:
		e.Hear(dev, Heard{Control: "resume", Button: true})
		e.mu.Lock()
		d.cut = false
		e.mu.Unlock()
		if cut {
			return nil // the song is pushed again from where it was
		}
		return e.sp.PlayerOp(ctx, dev, ControlPlay)
	case action == ControlPlay:
		return e.sp.PlayerOp(ctx, dev, ControlPlay)
	case action == ControlPause && (!on || watching):
		return e.pause(ctx, dev, d, gen, on)
	}
	// Stop. A song that is only being started cannot be paused, so that stops it too.
	e.mu.Lock()
	if d.cancel != nil {
		d.cancel(errStopped)
	}
	e.mu.Unlock()
	err := e.halt(ctx, dev)
	if err != nil && ctx.Err() == nil {
		err = e.halt(ctx, dev)
	}
	// A push may have been on its way, and some models need to be told again.
	go func() {
		defer logx.Recover(e.log, "音箱")
		bg := context.WithoutCancel(ctx)
		for i, quiet := 0, 0; i < 6 && quiet < 2; i++ {
			e.sleep(bg, pressed)
			e.mu.Lock()
			newer := d.gen != gen
			e.mu.Unlock()
			if newer {
				return
			}
			if p, err := e.sp.PlayStatus(bg, dev); err == nil && !p.Playing() {
				quiet++
				continue
			}
			quiet = 0
			e.halt(bg, dev)
		}
	}()
	return err
}

// pause pauses, asks again, and at last stops: a song of ours can be continued from there.
func (e *Engine) pause(ctx context.Context, dev string, d *device, gen int, ours bool) error {
	e.Hear(dev, Heard{Control: "pause", Button: true})
	var err error
	for range 2 {
		if err = e.sp.PlayerOp(ctx, dev, ControlPause); err == nil {
			e.sleep(ctx, pressed)
			if p, serr := e.sp.PlayStatus(ctx, dev); serr == nil && !p.Playing() {
				return nil
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	if !ours || ctx.Err() != nil {
		return err
	}
	if err = e.halt(ctx, dev); err != nil {
		e.Hear(dev, Heard{Control: "resume", Button: true}) // it goes on
		return err
	}
	e.mu.Lock()
	if d.gen == gen {
		d.cut = true
	}
	e.mu.Unlock()
	e.Hear(dev, Heard{Control: "pause", Button: true, Cut: true})
	return nil
}

// seek asks a run to move to a point of its song.
type seek struct {
	ctx  context.Context
	to   time.Duration
	done chan error
}

// Why a song stays where it is.
var (
	ErrCannotSeek = errors.New("这首歌的格式不能拖动")
	ErrNoSeek     = errors.New("没有能拖动的歌")
	ErrSeekLate   = errors.New("音箱没有跟上，稍后再试")
	ErrSeekMoved  = errors.New("音箱已经换歌，请对当前歌曲操作")
)

// seekFor is how long a speaker may take to move.
const seekFor = 14 * time.Second

// Seek moves the song on a speaker to a point. A paused song stays paused.
func (e *Engine) Seek(ctx context.Context, dev string, to time.Duration, runID ...int64) error {
	ctx, cancel := context.WithTimeout(ctx, seekFor)
	defer cancel()
	if ctx.Err() != nil {
		return ErrSeekLate
	}
	s := seek{ctx, max(0, to), make(chan error, 1)}
	e.mu.Lock()
	d := e.devs[dev]
	if d == nil || d.seeks == nil {
		e.mu.Unlock()
		return ErrNoSeek
	}
	if len(runID) != 0 && runID[0] != d.runID {
		e.mu.Unlock()
		return ErrSeekMoved
	}
	sent := false
	select {
	case d.seeks <- s:
		sent = true
		d.handOver()
	default:
	}
	e.mu.Unlock()
	if !sent {
		return ErrSeekLate
	}
	select {
	case err := <-s.done:
		return err
	case <-ctx.Done():
		return ErrSeekLate
	}
}

// SetVolume sets the volume. On a speaker that is muted for a moment it is the volume that
// comes back, so that Xiao AI's answer stays unheard.
func (e *Engine) SetVolume(ctx context.Context, dev string, volume int) error {
	if changed, err := e.st.ChangePendingVolume(ctx, dev, volume); err != nil || changed && e.held(dev) {
		return err
	}
	e.touch(dev)
	if err := e.sp.SetVolume(ctx, dev, volume); err != nil {
		return err
	}
	e.know(dev, volume)
	if !e.held(dev) {
		e.st.ClearPendingVolume(ctx, dev)
	}
	return nil
}

// Woke tells a song of ours that its speaker was woken.
func (e *Engine) Woke(dev string) {
	e.Release(dev)
	e.Hear(dev, Heard{Woke: true})
}

// Describe explains a failed control.
func Describe(err error) string { return describe(err) }
