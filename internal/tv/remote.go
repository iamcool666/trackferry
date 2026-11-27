package tv

import (
	"errors"
	"strconv"
	"sync"
	"time"
)

// A reference contains a catalog ID or a provider key. It never carries an audio URL.
type Ref struct {
	ID     int64  `json:"id,omitempty"`
	Online string `json:"online,omitempty"`
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
}

type Playback struct {
	Revision string `json:"revision"`
	Entry    string `json:"entry"`
	Current  Ref    `json:"current"`
	Index    int    `json:"index"`
	Count    int    `json:"count"`
	Position int64  `json:"position_ms"`
	Duration int64  `json:"duration_ms"`
	Phase    string `json:"phase"`
	Mode     string `json:"mode"`
	Seekable bool   `json:"seekable"`
	Volume   *int   `json:"volume,omitempty"`
	At       int64  `json:"at_ms"`
	Queue    []Ref  `json:"queue,omitempty"`
}

type Command struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Items     []Ref  `json:"items,omitempty"`
	Index     int    `json:"index,omitempty"`
	Position  int64  `json:"position_ms,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Revision  string `json:"revision,omitempty"`
	Entry     string `json:"entry,omitempty"`
	QueueMode string `json:"queue_mode,omitempty"`
	Volume    *int   `json:"volume,omitempty"`
	Expires   int64  `json:"expires_ms"`
}

type Result struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type Heartbeat struct {
	Epoch    string   `json:"epoch"`
	Sequence int64    `json:"sequence"`
	Playback Playback `json:"playback"`
	Result   *Result  `json:"result,omitempty"`
}

type Update struct {
	Command        *Command `json:"command,omitempty"`
	NeedQueue      bool     `json:"need_queue"`
	ResultAccepted bool     `json:"result_accepted"`
	ResultNew      bool     `json:"-"`
}

var (
	ErrOffline = errors.New("电视不在线，请先打开 TrackFerry TV")
	ErrMoved   = errors.New("播放列表已变化，请重试")
)

const OnlineFor = 8 * time.Second
const CommandFor = 24 * time.Second
const MaxQueue = 20000

type remoteDevice struct {
	epoch      string
	previous   [4]string
	sequence   int64
	playback   Playback
	seen       time.Time
	pending    *Command
	results    [16]Result
	nextResult int
}

func (d *remoteDevice) finish(result Result) {
	d.results[d.nextResult] = result
	d.nextResult = (d.nextResult + 1) % len(d.results)
	d.pending = nil
}

func (d *remoteDevice) result(id string) Result {
	for _, result := range d.results {
		if result.ID == id {
			return result
		}
	}
	return Result{ID: id, State: "superseded"}
}

// Remote is in-memory. Reconnects can repeat a report or a command receipt, while a
// new app session can never execute commands issued to the previous session.
type Remote struct {
	mu     sync.Mutex
	states map[string]*remoteDevice
	now    func() time.Time
	next   int64
}

func NewRemote() *Remote {
	return &Remote{states: map[string]*remoteDevice{}, now: time.Now, next: time.Now().UnixNano()}
}

func (r *Remote) expire(d *remoteDevice) {
	if d.pending != nil && d.pending.Expires <= r.now().UnixMilli() {
		d.finish(Result{d.pending.ID, "failed", "电视没有及时确认"})
	}
}

func (r *Remote) Sync(id string, h Heartbeat) Update {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.states[id]
	if d == nil {
		d = &remoteDevice{}
		r.states[id] = d
	}
	if d.epoch != h.Epoch {
		for _, old := range d.previous {
			if old == h.Epoch {
				return Update{}
			}
		}
		copy(d.previous[1:], d.previous[:])
		d.previous[0] = d.epoch
		if d.pending != nil {
			d.finish(Result{d.pending.ID, "failed", "电视已重新连接，请重试"})
		}
		d.epoch, d.sequence, d.pending, d.playback = h.Epoch, 0, nil, Playback{}
	}
	r.expire(d)
	resultNew := false
	if h.Sequence > d.sequence {
		if h.Playback.Revision == d.playback.Revision && h.Playback.Queue == nil {
			h.Playback.Queue = d.playback.Queue
		}
		h.Playback.At = r.now().UnixMilli()
		d.playback, d.sequence, d.seen = h.Playback, h.Sequence, r.now()
		if h.Result != nil && d.pending != nil && h.Result.ID == d.pending.ID {
			d.finish(*h.Result)
			resultNew = true
		}
	}
	return Update{
		Command:        d.pending,
		NeedQueue:      d.playback.Count > 0 && len(d.playback.Queue) != d.playback.Count,
		ResultAccepted: h.Result != nil && d.result(h.Result.ID) == *h.Result,
		ResultNew:      resultNew,
	}
}

func (r *Remote) State(id string, includeQueue bool) (Playback, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.states[id]
	if d == nil {
		return Playback{}, false
	}
	p := d.playback
	if !includeQueue {
		p.Queue = nil
	}
	return p, r.now().Sub(d.seen) <= OnlineFor
}

func (r *Remote) Submit(id string, c Command) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.states[id]
	if d == nil || r.now().Sub(d.seen) > OnlineFor {
		return Result{}, ErrOffline
	}
	if c.Revision != "" && c.Revision != d.playback.Revision || c.Entry != "" && c.Entry != d.playback.Entry {
		return Result{}, ErrMoved
	}
	r.next++
	c.ID, c.Expires = strconv.FormatInt(r.next, 36), r.now().Add(CommandFor).UnixMilli()
	if d.pending != nil {
		d.finish(Result{ID: d.pending.ID, State: "superseded"})
	}
	d.pending = &c
	return Result{ID: c.ID, State: "pending"}, nil
}

func (r *Remote) Status(id, command string) Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.states[id]
	if d != nil {
		r.expire(d)
		if d.pending != nil && d.pending.ID == command {
			return Result{ID: command, State: "pending"}
		}
		return d.result(command)
	}
	return Result{ID: command, State: "superseded"}
}

func (r *Remote) Cancel(id, command string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.states[id]; d != nil && d.pending != nil && d.pending.ID == command {
		d.finish(Result{ID: command, State: "superseded"})
	}
}

// Gone marks a device offline at once; its next report brings it back.
func (r *Remote) Gone(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.states[id]; d != nil {
		d.seen = time.Time{}
	}
}

func (r *Remote) Revoke(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, id)
}
