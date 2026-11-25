package takeover

import (
	"context"
	"fmt"
	"time"

	"trackferry/internal/xiaomi"
)

const (
	phasePlaying  = "playing"
	phasePaused   = "paused"
	phaseTalking  = "talking"
	phaseResuming = "resuming"
)

// maxPause is how long a paused song is waited for.
const maxPause = 15 * time.Minute

// minStart is the earliest point a song is started from as its middle.
const minStart = 200 * time.Millisecond

func minutes(d time.Duration) string {
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// startLag is from the speaker's first request to the first sound.
const startLag = 500 * time.Millisecond

// watch follows our song until it is over. It keeps the song's clock, because speakers do not
// report a position for pushed audio; it continues the song after someone talked to Xiao AI;
// and it stops the speaker once the song has played through, so it does not repeat. A newer
// run on the speaker cancels the watch (and owns the speaker).
func (r *runner) watch(fetchedAt time.Time, heard <-chan Heard, seeks <-chan seek) (how End) {
	e, t := r.e, r.req.Track
	tm := e.timing
	if tm.Look <= 0 {
		tm.Look, tm.LookClose = DefaultTiming.Look, DefaultTiming.LookClose
	}
	length := t.Length
	if length <= 0 {
		length = t.Duration
	}
	track := t
	var ended <-chan time.Time
	if t.Duration > 0 {
		x := time.NewTimer(time.Until(fetchedAt.Add(t.Duration + tm.EndSlack)))
		defer x.Stop()
		ended = x.C
	}
	limit := time.NewTimer(tm.MaxWatch)
	defer limit.Stop()

	base := time.Duration(0)          // played before anchor
	anchor := fetchedAt.Add(startLag) // sounding since
	sounding := true
	at := func() time.Duration {
		if !sounding {
			return base
		}
		return base + max(0, e.now().Sub(anchor))
	}
	var (
		talkedUntil time.Time // someone talked; Xiao AI is done about then
		silent      int       // status reads in a row without sound
		foreign     int
		silently    bool // waiting for a button to take effect, not for someone talking
		soon        bool // look at the speaker at once
		deaf        bool // paused by a button; a speaker that still claims to play is not believed
		byUser      bool // the listener wants quiet
		cut         bool // and only a new push brings it back
		yield       bool // Xiao AI was asked to play something
		pausedAt    time.Time
		resumes     int
		sought      bool // base is where the listener put it
		lastOK      = e.now()
	)
	pause := func(when time.Time) {
		if !sounding {
			return
		}
		base = max(0, base+when.Sub(anchor))
		if length > 0 {
			base = min(base, length)
		}
		sounding, pausedAt = false, e.now()
	}
	play := func(from time.Duration, when time.Time) {
		base, anchor, sounding, pausedAt = from, when, true, time.Time{}
	}
	over := func(step, why string) { r.phase = ""; r.step(step, true, why); r.status() }
	// rest waits for a conversation over the end of the song and reports whether another song
	// may follow.
	rest := func() bool {
		take := func(h Heard) {
			switch {
			case h.Control == "resume":
			case h.Control != "" || h.Stop || h.Audio:
				byUser = true
			case h.Woke:
				talkedUntil = e.now().Add(15 * time.Second)
			default:
				talkedUntil = h.At.Add(tm.Pause + min(45*time.Second, time.Duration(h.Answer)*tm.Speak))
			}
		}
		for r.ctx.Err() == nil {
			select {
			case h := <-heard:
				take(h)
				continue
			default:
			}
			left := talkedUntil.Sub(e.now())
			if byUser || yield || talkedUntil.IsZero() || left <= 0 {
				return !byUser && !yield
			}
			wait := time.NewTimer(left)
			select {
			case <-r.ctx.Done():
			case <-wait.C:
			case h := <-heard:
				take(h)
			}
			wait.Stop()
		}
		return false
	}
	finish := func(why string) {
		p, err := r.status()
		if err == nil && r.foreign(p) {
			r.step(StepEnded, true, why+"，音箱在放别的，不动它")
			return
		}
		defer func() {
			if rest() {
				how = EndPlayed
			}
		}()
		if err == nil && !p.Playing() {
			r.step(StepEnded, true, why+"，音箱已经停了")
			return
		}
		// Left alone the speaker would repeat the song for good.
		for i := 0; i < 4; i++ {
			e.sp.PlayerOp(r.ctx, r.req.DeviceID, "pause")
			if err = e.sp.PlayerOp(r.ctx, r.req.DeviceID, "stop"); err == nil || e.sleep(r.ctx, time.Duration(i+1)*time.Second) != nil {
				break
			}
		}
		r.step(StepEnded, err == nil, why+"，停止循环 "+describe(err))
	}
	// put has the speaker play n, the song from start on.
	put := func(n Track, start time.Duration) bool {
		err := e.sp.PlayURL(r.ctx, r.req.DeviceID, r.req.Hardware, n.URL)
		for i := 0; err != nil && i < 3 && e.sleep(r.ctx, time.Duration(i+1)*400*time.Millisecond) == nil; i++ {
			err = e.sp.PlayURL(r.ctx, r.req.DeviceID, r.req.Hardware, n.URL)
		}
		if err != nil {
			r.step(StepResume, false, describe(err))
			return false
		}
		select {
		case <-n.Fetched: // nil when starting over: wait a moment instead
		case <-time.After(6 * time.Second):
			if n.Fetched != nil {
				r.step(StepResume, false, "音箱没有来取流")
				return false
			}
		case <-r.ctx.Done():
			return false
		}
		track = n
		play(start, e.now().Add(startLag))
		r.phase = phasePlaying
		return true
	}
	// again pushes the song from where it stopped.
	again := func() bool {
		if t.Resume == nil || resumes >= 20 {
			return false
		}
		resumes++
		from := max(time.Second, base-1500*time.Millisecond)
		if sought {
			from = max(minStart, base) // exactly where the listener put it
		}
		r.phase = phaseResuming
		n, start, err := t.Resume(r.ctx, from)
		if err != nil { // a format that only starts at its beginning
			start = 0
			n = Track{URL: t.URL, Lost: t.Lost}
		}
		if !put(n, start) {
			return false
		}
		sought = false
		if start == 0 {
			r.step(StepResume, true, "这个格式只能从头放")
		} else {
			r.step(StepResume, true, "从 "+minutes(start)+" 接着放")
		}
		return true
	}

	setCut := func(on bool) {
		cut = on
		e.mu.Lock()
		if d := e.devs[r.req.DeviceID]; d != nil && d.seeks != nil && (<-chan seek)(d.seeks) == seeks {
			d.cut = on
		}
		e.mu.Unlock()
	}
	// move goes to another point of the song. What cannot start there plays on as it is.
	move := func(to time.Duration) error {
		if t.Resume == nil || !t.Seekable || length <= 0 {
			return ErrCannotSeek
		}
		to = min(max(minStart, to), max(minStart, length-2*time.Second))
		if !sounding && talkedUntil.IsZero() {
			base, sought = to, true
			setCut(true) // it goes on from there when it goes on
			r.elapsed = base
			r.step(StepSeek, true, "停着，放到 "+minutes(to))
			return nil
		}
		n, start, err := t.Resume(r.ctx, to)
		if err != nil {
			r.step(StepSeek, false, "这个格式不能拖动")
			return ErrCannotSeek
		}
		was, since, held, stopped := base, anchor, !sounding, pausedAt
		if !put(n, start) {
			base, anchor, sounding, pausedAt = was, since, !held, stopped
			return ErrSeekLate
		}
		talkedUntil, silent, foreign, sought = time.Time{}, 0, 0, false
		byUser, deaf, yield = false, false, false
		r.elapsed, r.held = at(), false
		r.step(StepSeek, true, "拖到 "+minutes(start))
		return nil
	}
	defer func() {
		for {
			select {
			case s := <-seeks:
				s.done <- ErrNoSeek
			default:
				return
			}
		}
	}()

	timer := time.NewTimer(tm.Look)
	defer timer.Stop()
	r.phase = phasePlaying
	for {
		close := !talkedUntil.IsZero() || (!sounding && e.now().Sub(pausedAt) < 20*time.Second)
		if close {
			timer.Reset(tm.LookClose)
		} else {
			look := tm.Look
			if !sounding && e.now().Sub(pausedAt) > 2*time.Minute {
				look = max(look, min(10*time.Second, 4*look))
			}
			timer.Reset(look)
		}
		if soon {
			timer.Reset(tm.LookClose / 4)
			soon = false
		}
		select {
		case <-r.ctx.Done():
			r.phase = ""
			if context.Cause(r.ctx) == errStopped {
				r.step(StepStopped, true, "按了停止")
				return
			}
			r.step(StepCancelled, true, "")
			return
		case <-limit.C:
			return
		case <-ended:
			finish("播完了")
			return
		case <-track.Restarted:
			if length > 0 && at() < length*8/10 {
				track.Restarted = nil // it went back to the beginning; the song is not over
				play(0, e.now().Add(startLag))
				continue
			}
			finish("音箱又从头放了")
			return
		case s := <-seeks:
			s.done <- move(s.to)
			soon = true
			continue
		case h := <-heard:
			soon, silently = h.Button, h.Button
			switch {
			case h.Woke:
				if !byUser { // if nothing is said to it, the song goes on
					talkedUntil = e.now().Add(15 * time.Second)
				}
			case h.Control == "stop":
				over(StepStopped, "说了停止")
				return
			case h.Control == "pause", h.Stop:
				byUser, talkedUntil = true, time.Time{}
				cut = cut || h.Cut
				if h.Button && !h.Cut {
					deaf = true
					if sounding {
						pause(e.now())
						r.phase, r.elapsed, r.held = phasePaused, base, true
						r.step(StepPaused, true, "按了暂停")
					}
				}
			case h.Control == "resume":
				byUser, deaf = false, false
				if cut {
					setCut(false)
					if !again() && r.ctx.Err() == nil {
						over(StepStopped, "没能接着放")
						return
					}
					continue
				}
				talkedUntil = e.now().Add(tm.Pause)
			case h.Control != "", h.Audio:
				yield, talkedUntil = true, time.Time{}
			default:
				byUser, yield = false, false
				if !h.Quiet {
					r.step(StepTalked, true, "")
				}
				talkedUntil = h.At.Add(tm.Pause + min(45*time.Second, time.Duration(h.Answer)*tm.Speak))
				if x := e.now().Add(tm.Pause); talkedUntil.Before(x) {
					talkedUntil = x
				}
			}
			continue
		case <-timer.C:
		}

		talking := !talkedUntil.IsZero() && e.now().Before(talkedUntil)
		switch {
		case talking && !silently:
			r.phase = phaseTalking
		case sounding:
			r.phase = phasePlaying
		default:
			r.phase = phasePaused
		}
		r.elapsed, r.held = at(), !sounding
		p, err := r.status()
		if err != nil {
			continue
		}
		now := e.now()
		lost := track.Lost != nil && track.Lost()
		// Xiao AI's voice can read as "playing" too; a speaker that hung up is not playing ours.
		ours := p.Playing() && !r.foreign(p) && !(lost && (talking || !sounding))
		if deaf {
			ours = false
			if !p.Playing() {
				deaf = false // it did pause; from here on its word counts again
			}
		}
		switch {
		case ours:
			silent, foreign = 0, 0
			if !sounding && !talking && sought { // started on the speaker itself
				setCut(false)
				if again() {
					byUser, lastOK = false, now
					continue
				}
			}
			if !sounding && !talking {
				play(base, lastOK.Add(now.Sub(lastOK)/2))
				r.phase = phasePlaying
				byUser = false
				r.step(StepContinued, true, "")
			}
			if !talkedUntil.IsZero() && now.After(talkedUntil.Add(2*time.Second)) {
				talkedUntil = time.Time{}
			}
			if length > 0 && at() > length+8*time.Second {
				finish("播完了")
				return
			}
		default:
			if p.Playing() && (!deaf || r.foreign(p)) {
				foreign++
			} else {
				foreign = 0
			}
			silent++
			if sounding {
				when := lastOK.Add(now.Sub(lastOK) / 2)
				if length > 0 && at() > length-6*time.Second {
					r.phase = ""
					finish("播完了")
					return
				}
				pause(when)
				r.phase = phasePaused
				if talking && !silently {
					r.phase = phaseTalking
				}
				r.elapsed = base
				r.step(StepPaused, true, "")
			}
			switch {
			case yield && foreign >= 2, yield && now.Sub(pausedAt) > 15*time.Second:
				over(StepStopped, "小爱在放别的")
				return
			case foreign >= 12:
				over(StepStopped, "音箱在放别的")
				return
			case byUser || talkedUntil.IsZero():
				if now.Sub(pausedAt) > maxPause {
					over(StepStopped, "停了很久，不再等")
					return
				}
			case !talking && silent >= 2 && !p.Playing():
				talkedUntil, silent = time.Time{}, 0
				if !again() && r.ctx.Err() == nil {
					over(StepStopped, "没能接着放")
					return
				}
			}
		}
		lastOK = now
	}
}

// foreign reports that the speaker plays something that is certainly not ours.
func (r *runner) foreign(p xiaomi.PlayStatus) bool {
	return p.Playing() && xiaomi.UsesMusicAPI(r.req.Hardware) && p.AudioID != "" && p.AudioID != xiaomi.OurAudioID
}
