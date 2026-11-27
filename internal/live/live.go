// Package live tells the web UI what is happening on the speakers while it happens. It only
// reports what other packages observed and decides nothing. Nothing here is stored: finished
// requests are in the database.
package live

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"
)

// Flow is one request on its way from a speaker's microphone to its loudspeaker.
type Flow struct {
	ID         int64          `json:"id"`
	Kind       string         `json:"kind"` // command | test | try
	DeviceID   string         `json:"device_id,omitempty"`
	Device     string         `json:"device,omitempty"`
	CommandID  int64          `json:"command_id,omitempty"`
	Said       string         `json:"said"`
	By         string         `json:"by,omitempty"`          // the browser that put it on: 客厅 iPad
	UpstreamMS int64          `json:"upstream_ms,omitempty"` // Xiaomi understood the words
	SeenMS     int64          `json:"seen_ms"`
	Intent     any            `json:"intent,omitempty"`
	Layers     map[string]any `json:"layers"`
	Route      any            `json:"route,omitempty"`
	RouteMS    int64          `json:"route_ms,omitempty"` // when the decision stood
	Run        any            `json:"run,omitempty"`
	Audio      *Audio         `json:"audio,omitempty"`
	Player     *Player        `json:"player,omitempty"`
	Done       bool           `json:"done"`
	Note       string         `json:"note,omitempty"`
	UpdatedMS  int64          `json:"updated_ms"`
}

// Audio is what the speaker fetched from us.
type Audio struct {
	Bytes    int64 `json:"bytes"`
	Total    int64 `json:"total,omitempty"`
	Requests int   `json:"requests"`
	Open     bool  `json:"open"`
	AtMS     int64 `json:"at_ms"`
}

// Player is the speaker's own report.
type Player struct {
	Status   int   `json:"status"`
	Position int64 `json:"position"`
	Duration int64 `json:"duration"`
	Volume   int   `json:"volume"`
	Ours     bool  `json:"ours"`
	Held     bool  `json:"held,omitempty"` // our clock stands still
	// Elapsed is how far our song has played, by our own clock; speakers rarely report it.
	Elapsed int64  `json:"elapsed"`
	Phase   string `json:"phase,omitempty"` // playing | paused | talking | resuming
	Seek    bool   `json:"seek,omitempty"`  // it can be moved within the song
	AtMS    int64  `json:"at_ms"`
}

// Pulse is one look at a speaker's conversation records.
type Pulse struct {
	Every    int64  `json:"every_ms"` // time between looks
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	OK       bool   `json:"ok"`
	MS       int64  `json:"ms"`
	Ignored  int    `json:"ignored"` // utterances that were not requests; their words are not kept
	Recorded int    `json:"recorded"`
	AtMS     int64  `json:"at_ms"`
}

const keep = 12

// Hub is safe for concurrent use. A nil Hub does nothing.
type Hub struct {
	mu     sync.Mutex
	next   int64
	flows  []*Flow // oldest first
	pulses map[string]Pulse
	subs   map[chan []byte]struct{}
	wakes  map[chan struct{}]struct{}
}

// New returns an empty Hub.
func New() *Hub {
	return &Hub{next: time.Now().UnixMilli(), pulses: map[string]Pulse{}, subs: map[chan []byte]struct{}{}, wakes: map[chan struct{}]struct{}{}}
}

// Watching reports whether anyone is looking.
func (h *Hub) Watching() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs) > 0
}

// Subscribe returns messages ready to send. The channel is closed when the reader is too slow.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 64)
	ch <- encode("hello", map[string]any{"now_ms": time.Now().UnixMilli(), "flows": h.flows, "pulses": h.pulses})
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func encode(event string, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return append(append([]byte("event: "+event+"\ndata: "), b...), "\n\n"...)
}

// send needs h.mu.
func (h *Hub) send(event string, v any) {
	if event == "flow" {
		for ch := range h.wakes {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	if len(h.subs) == 0 {
		return
	}
	msg := encode(event, v)
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Changes returns a channel that says some flow changed since it was last read.
func (h *Hub) Changes() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.wakes[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.wakes, ch)
	}
}

// Flows returns the flows as they are now, oldest first, without their layers.
func (h *Hub) Flows() []Flow {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Flow, 0, len(h.flows))
	for _, f := range h.flows {
		c := *f
		c.Layers = nil
		out = append(out, c)
	}
	return out
}

// Pulse reports one look at a speaker.
func (h *Hub) Pulse(p Pulse) {
	if h == nil {
		return
	}
	p.AtMS = time.Now().UnixMilli()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pulses[p.DeviceID] = p
	h.send("pulse", p)
}

// Begin starts a flow.
func (h *Hub) Begin(f Flow) *Trace {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	f.ID, f.Layers, f.UpdatedMS = h.next, map[string]any{}, time.Now().UnixMilli()
	if f.SeenMS == 0 {
		f.SeenMS = f.UpdatedMS
	}
	h.flows = append(h.flows, &f)
	if len(h.flows) > keep {
		// A song that is still on outlives newer requests.
		i := max(0, slices.IndexFunc(h.flows[:len(h.flows)-1], func(x *Flow) bool { return x.Done }))
		h.flows = slices.Delete(slices.Clone(h.flows), i, i+1)
	}
	h.send("flow", &f)
	return &Trace{h, &f}
}

// MarkBy names the browser that put on what a speaker's newest flow plays.
func (h *Hub) MarkBy(dev, by string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.flows) - 1; i >= 0; i-- {
		if f := h.flows[i]; f.DeviceID == dev {
			if f.By != by {
				f.By, f.UpdatedMS = by, time.Now().UnixMilli()
				h.send("flow", f)
			}
			return
		}
	}
}

// Trace writes to one flow. A nil Trace does nothing.
type Trace struct {
	h *Hub
	f *Flow
}

func (t *Trace) edit(f func(*Flow)) {
	if t == nil {
		return
	}
	t.h.mu.Lock()
	defer t.h.mu.Unlock()
	f(t.f)
	t.f.UpdatedMS = time.Now().UnixMilli()
	t.h.send("flow", t.f)
}

// The values handed over below must not change afterwards.

// Layer reports one source.
func (t *Trace) Layer(name string, v any) { t.edit(func(f *Flow) { f.Layers[name] = v }) }

// Route reports the decision.
func (t *Trace) Route(v any) {
	t.edit(func(f *Flow) { f.Route, f.RouteMS = v, time.Now().UnixMilli() })
}

// Run reports a takeover's progress.
func (t *Trace) Run(v any) { t.edit(func(f *Flow) { f.Run = v }) }

// Audio reports what the speaker fetched.
func (t *Trace) Audio(a Audio) {
	a.AtMS = time.Now().UnixMilli()
	t.edit(func(f *Flow) { f.Audio = &a })
}

// Player reports what the speaker says it plays.
func (t *Trace) Player(p Player) {
	p.AtMS = time.Now().UnixMilli()
	t.edit(func(f *Flow) { f.Player = &p })
}

// Done ends the flow.
func (t *Trace) Done(note string) {
	t.edit(func(f *Flow) {
		f.Done = true
		if note != "" {
			f.Note = note
		}
	})
}

// Watching reports whether anyone is looking.
func (t *Trace) Watching() bool { return t != nil && t.h.Watching() }

type key struct{}

// With attaches a trace to ctx.
func With(ctx context.Context, t *Trace) context.Context { return context.WithValue(ctx, key{}, t) }

// From returns ctx's trace, or nil.
func From(ctx context.Context) *Trace {
	t, _ := ctx.Value(key{}).(*Trace)
	return t
}
