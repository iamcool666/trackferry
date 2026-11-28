// Package online finds songs on online platforms through their own public endpoints.
//
// Songs the platform restricts are never resolved by other means: no paid-content parsers.
// The one account used is the owner's own, on 网易云. What comes back is probed, and trial
// clips are refused.
package online

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"trackferry/internal/audio"
	"trackferry/internal/names"
)

// Reading is one way to understand a request. Artist may be empty.
type Reading struct {
	Title  string
	Artist string
}

// Candidate is a song a platform offers.
type Candidate struct {
	Source     string        `json:"source"`
	ID         string        `json:"-"`
	Key        string        `json:"key,omitempty"` // names it to Take
	ref        string        // second platform ID, when resolving needs one
	Title      string        `json:"title"`
	Artist     string        `json:"artist"`
	Duration   time.Duration `json:"-"`
	DurationMS int64         `json:"duration_ms"`
	Restricted bool          `json:"restricted"` // members only or paid
	Member     bool          `json:"-"`          // played by the rights of an account: never kept
	Note       string        `json:"note,omitempty"`
	Why        string        `json:"why,omitempty"` // not the song asked for
	Album      string        `json:"album,omitempty"`
	Cover      string        `json:"cover,omitempty"` // NetCover ID of the platform's picture
	lyric      string        // where the platform keeps the lyrics, when the search says
}

// Stream is a resolved, probed audio address.
type Stream struct {
	URL        string
	Duration   time.Duration
	Type       string
	Kbps       int // read from the audio itself, not what the platform claims
	SampleRate int
	Bits       int
}

// Source is one platform.
type Source interface {
	Name() string
	Search(ctx context.Context, query string) ([]Candidate, error)
	// Resolve returns the audio address of a candidate (not probed yet).
	Resolve(ctx context.Context, c Candidate) (string, error)
}

// Kinds of failure.
var (
	ErrRestricted = errors.New("会员或付费歌曲")
	ErrTrial      = errors.New("只有试听片段")
	ErrNoAudio    = errors.New("平台没有给出可播音频")
	ErrLimited    = errors.New("平台限流")
)

// States of a layer.
const (
	StateHit        = "hit"
	StateMiss       = "miss"
	StateAmbiguous  = "ambiguous"
	StateRestricted = "restricted" // found, but no full audio may be played
	StateError      = "error"
	StateTimeout    = "timeout"
	StateDisabled   = "disabled"
	StateOpen       = "open"    // circuit breaker: not asked
	StateSkipped    = "skipped" // not needed
)

// Outcome is one platform's part in a lookup.
type Outcome struct {
	Source     string      `json:"source"`
	State      string      `json:"state"`
	MS         int64       `json:"ms"`
	Detail     string      `json:"detail,omitempty"`
	Candidates []Candidate `json:"candidates"`
	Near       bool        `json:"-"` // what it has is only like what was said
}

// Found is a playable song.
type Found struct {
	Candidate Candidate
	Stream    Stream
	Near      bool // only if a character was misheard
	// Unprobed is a stream taken as it is, without being asked what it holds. Its source
	// vouches for it: the platforms hand out their own encodings (a 酷我 .mflac, a 酷狗 MP3
	// under another name, an Ogg), which say nothing about whether the song will play.
	Unprobed bool
}

// Health is a platform's recent record.
type Health struct {
	Source    string `json:"source"`
	Enabled   bool   `json:"enabled"`
	OK        int    `json:"ok"`
	Failed    int    `json:"failed"`
	LastMS    int64  `json:"last_ms"`
	LastAt    int64  `json:"last_at_ms"`
	LastError string `json:"last_error,omitempty"`
	OpenUntil int64  `json:"open_until_ms,omitempty"`
}

type entry struct {
	src     Source
	enabled bool
	h       Health
	fails   int
	open    time.Time
}

// Limits of one lookup.
const (
	searchTimeout  = 2200 * time.Millisecond
	totalBudget    = 5 * time.Second
	breakAfter     = 4
	breakFor       = time.Minute
	minFullShare   = 0.85
	maxPerSource   = 3
	shortestSong   = 45 * time.Second
	shownPerSource = 6
)

// Service asks all platforms. It is safe for concurrent use.
type Service struct {
	log   *slog.Logger
	probe *http.Client
	mu    sync.Mutex
	srcs  []*entry
	sem   chan struct{}

	paceMu  sync.Mutex
	pace    map[string]*pacer
	offMu   sync.Mutex
	offered map[string]Candidate // shown by Search, until picked
	order   []string

	quality atomic.Value // Quality
	techMu  sync.Mutex
	tech    map[string]Tech // what each played song really was

	// Remember keeps the songs a search showed beyond this process; Recall finds one again.
	// Both optional: without them a song can be picked only while it is among the last shown.
	Remember func(map[string]Saved)
	Recall   func(key string) (Saved, bool)
}

// New returns a Service over sources in order of preference, all enabled.
func New(log *slog.Logger, client *http.Client, sources ...Source) *Service {
	s := &Service{log: log, probe: client, sem: make(chan struct{}, 4)}
	for _, x := range sources {
		s.srcs = append(s.srcs, &entry{src: x, enabled: true, h: Health{Source: x.Name()}})
	}
	return s
}

// SetEnabled switches a platform; false for an unknown name.
func (s *Service) SetEnabled(name string, on bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.srcs {
		if e.src.Name() == name {
			e.enabled = on
			return true
		}
	}
	return false
}

// Health lists all platforms.
func (s *Service) Health() []Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Health{}
	for _, e := range s.srcs {
		h := e.h
		h.Enabled = e.enabled
		if time.Now().Before(e.open) {
			h.OpenUntil = e.open.UnixMilli()
		}
		out = append(out, h)
	}
	return out
}

func (s *Service) record(e *entry, took time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.h.LastMS, e.h.LastAt = took.Milliseconds(), time.Now().UnixMilli()
	if err == nil {
		e.h.OK++
		e.fails, e.h.LastError = 0, ""
		return
	}
	e.h.Failed++
	e.h.LastError = describe(err)
	if e.fails++; e.fails >= breakAfter {
		e.open, e.fails = time.Now().Add(breakFor), 0
	}
}

// Describe is err for people, without addresses.
func Describe(err error) string { return describe(err) }

func describe(err error) string {
	var r refused
	if errors.As(err, &r) {
		return r.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "超时"
	}
	for _, k := range []error{ErrRestricted, ErrTrial, ErrNoAudio, ErrLimited} {
		if errors.Is(err, k) {
			return k.Error()
		}
	}
	var he httpError
	if errors.As(err, &he) {
		return he.Error()
	}
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return "超时"
		}
		return "连不上平台"
	}
	return "平台返回了看不懂的内容"
}

// reason is describe with what the platform's answer looked like, for the log. Other errors
// may carry addresses.
func reason(err error) string {
	for _, k := range []error{ErrRestricted, ErrTrial, ErrNoAudio} {
		if errors.Is(err, k) {
			return err.Error()
		}
	}
	return describe(err)
}

type progressKey struct{}

// WithProgress has f called with every change of a platform's outcome during Find, from
// several goroutines.
func WithProgress(ctx context.Context, f func(Outcome)) context.Context {
	return context.WithValue(ctx, progressKey{}, f)
}

// StateAsking is an outcome still being worked out.
const StateAsking = "asking"

type hit struct {
	c     Candidate
	named bool // the request names the artist
	near  bool // only if a character was misheard
}

// Find looks a song up on every enabled platform. request is the whole utterance.
func (s *Service) Find(ctx context.Context, readings []Reading, request string) (*Found, []Outcome) {
	tell, _ := ctx.Value(progressKey{}).(func(Outcome))
	if tell == nil {
		tell = func(Outcome) {}
	}
	ctx, cancel := context.WithTimeout(ctx, totalBudget)
	defer cancel()
	outs := make([]Outcome, len(s.srcs))
	hits := make([][]hit, len(s.srcs))
	query := strings.TrimSpace(readings[0].Title + " " + readings[0].Artist)
	if len(readings) > 1 {
		query = readings[len(readings)-1].Title // the whole phrase
	}
	var wg sync.WaitGroup
	for i, e := range s.srcs {
		outs[i] = Outcome{Source: e.src.Name(), Candidates: []Candidate{}}
		s.mu.Lock()
		on, open := e.enabled, time.Now().Before(e.open)
		s.mu.Unlock()
		if !on {
			outs[i].State = StateDisabled
			continue
		}
		if open {
			outs[i].State, outs[i].Detail = StateOpen, "连续失败，暂停询问一会儿"
			continue
		}
		wg.Go(func() {
			start := time.Now()
			tell(Outcome{Source: e.src.Name(), State: StateAsking, Detail: "正在搜", Candidates: []Candidate{}})
			sctx, done := context.WithTimeout(ctx, searchTimeout)
			defer done()
			s.sem <- struct{}{}
			cands, err := s.search(sctx, e.src, query)
			<-s.sem
			if ctx.Err() == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				s.record(e, time.Since(start), err)
			}
			outs[i].MS = time.Since(start).Milliseconds()
			if err != nil {
				outs[i].State, outs[i].Detail = StateError, describe(err)
				if outs[i].Detail == "超时" {
					outs[i].State = StateTimeout
				}
				tell(outs[i])
				return
			}
			outs[i].Candidates = slices.Clone(cands[:min(shownPerSource, len(cands))])
			hits[i] = choose(cands, readings, request)
			explain(outs[i].Candidates, hits[i], readings, request)
			if len(hits[i]) == 0 {
				outs[i].State = StateMiss
				if n := len(cands); n > 0 {
					outs[i].Detail = fmt.Sprintf("搜到 %d 首，没有一首对得上", n)
				}
				tell(outs[i])
			} else {
				o := outs[i]
				o.State, o.Detail = StateAsking, "正在核对能不能放"
				tell(o)
			}
		})
	}
	wg.Wait()

	// A platform that has what was said outweighs those that only have something like it.
	exact := slices.ContainsFunc(hits, func(hs []hit) bool { return len(hs) > 0 && !hs[0].near })
	for i := range hits {
		if len(hits[i]) == 0 || !hits[i][0].near {
			continue
		}
		outs[i].Near = true
		if exact {
			hits[i] = nil
			outs[i].State, outs[i].Detail = StateMiss, "只有差一个字的，另一家有完全对得上的"
			tell(outs[i])
		}
	}

	// Nobody named the singer: the platforms' first choices should agree whose song it is.
	// When they do not, the more preferred platform's choice is tried first.
	split := false
	var first *hit
	for i := range hits {
		if len(hits[i]) == 0 || hits[i][0].named {
			continue
		}
		h := &hits[i][0]
		if first == nil {
			first = h
			continue
		}
		if !shareArtist(first.c.Artist, h.c.Artist) {
			split = true
			break
		}
	}

	type result struct {
		i int
		f *Found
	}
	rctx, stop := context.WithCancel(ctx)
	defer stop()
	ch := make(chan result, len(s.srcs))
	turn := make(chan struct{}) // closed when the next platform may go, if they take turns
	close(turn)
	n := 0
	for i, e := range s.srcs {
		if len(hits[i]) == 0 {
			continue
		}
		n++
		mine, next := turn, make(chan struct{})
		if split {
			turn = next
		}
		go func() {
			defer close(next)
			<-mine
			start := time.Now()
			f, state, detail := s.resolve(rctx, e, hits[i])
			outs[i].State, outs[i].Detail = state, detail
			outs[i].MS += time.Since(start).Milliseconds()
			if f != nil && f.Near {
				outs[i].Detail = "按听错一个字找到《" + f.Candidate.Title + "》"
			}
			if split && f != nil {
				outs[i].Detail = "没说歌手，原唱说法不一，先放这家的首选"
				stop()
			}
			if f != nil || rctx.Err() == nil {
				tell(outs[i])
			}
			ch <- result{i, f}
		}()
	}
	var found *Found
	for range n {
		r := <-ch // every worker ends by the budget
		if r.f != nil && found == nil {
			found = r.f
			stop()
		} else if found != nil && outs[r.i].State != StateRestricted {
			outs[r.i].State, outs[r.i].Detail = StateSkipped, "另一家先给出了完整音频"
			tell(outs[r.i])
		}
	}
	return found, outs
}

func shareArtist(a, b string) bool {
	for _, x := range names.Artists(a) {
		for _, y := range names.Artists(b) {
			if x == y {
				return true
			}
		}
	}
	return false
}

// choose keeps the candidates that are the requested song, in the platform's order.
func choose(cands []Candidate, readings []Reading, request string) []hit {
	var named, plain, nearNamed, nearPlain []hit
	canon, nearCanon := "", ""
	for _, c := range cands {
		if names.VersionPenalty(c.Title, request) > 0 || strings.Contains(c.Title, "免费试听") {
			continue
		}
		t, _ := names.SplitTitle(c.Title)
		titles := []string{names.Norm(c.Title), names.Norm(t)}
		ok, withArtist, near, nearArtist := false, false, false, false
		for _, r := range readings {
			want := names.Norm(r.Title)
			for _, title := range titles {
				if title == "" {
					continue
				}
				if r.Artist != "" {
					if names.ArtistMatch(c.Artist, r.Artist) {
						withArtist = withArtist || title == want
						nearArtist = nearArtist || names.NearTitle(title, want, true)
					} else if title == want {
						nearArtist = nearArtist || names.CreditNear(c.Artist, r.Artist)
					}
					continue
				}
				ok = ok || title == want
				near = near || names.NearTitle(title, want, false)
				for _, a := range names.Artists(c.Artist) {
					withArtist = withArtist || a+title == want || title+a == want
					if rest, cut := strings.CutPrefix(want, a); cut {
						nearArtist = nearArtist || names.NearTitle(title, rest, true)
					}
				}
			}
		}
		switch {
		case withArtist:
			named = append(named, hit{c: c, named: true})
		case ok && (canon == "" || shareArtist(canon, c.Artist)):
			canon = c.Artist
			plain = append(plain, hit{c: c})
		case nearArtist:
			nearNamed = append(nearNamed, hit{c: c, named: true, near: true})
		case near && (nearCanon == "" || shareArtist(nearCanon, c.Artist)):
			nearCanon = c.Artist
			nearPlain = append(nearPlain, hit{c: c, near: true})
		}
	}
	// What was misheard only counts when nothing is as it was said.
	for _, hs := range [][]hit{named, plain, nearNamed, nearPlain} {
		if len(hs) > 0 {
			return hs
		}
	}
	return nil
}

// explain notes why shown candidates are not the song asked for.
func explain(shown []Candidate, hs []hit, readings []Reading, request string) {
next:
	for i := range shown {
		c := &shown[i]
		for _, h := range hs {
			if h.c.ID == c.ID {
				continue next
			}
		}
		t, _ := names.SplitTitle(c.Title)
		same := false
		for _, r := range readings {
			same = same || names.Norm(r.Title) == names.Norm(c.Title) || names.Norm(r.Title) == names.Norm(t)
		}
		switch {
		case names.VersionPenalty(c.Title, request) > 0 || strings.Contains(c.Title, "免费试听"):
			c.Why = "现场、翻唱、试听这类版本"
		case !same:
			c.Why = "歌名对不上"
		default:
			c.Why = "歌手对不上"
		}
	}
}

func (s *Service) resolve(ctx context.Context, e *entry, hs []hit) (*Found, string, string) {
	state, detail := StateRestricted, ErrRestricted.Error()
	for i, h := range hs {
		if i == maxPerSource {
			break
		}
		if ctx.Err() != nil {
			return nil, StateSkipped, ""
		}
		if h.c.Restricted {
			continue
		}
		st, err := s.resolveOne(ctx, e.src, h.c)
		if err == nil {
			return &Found{Candidate: h.c, Stream: st, Near: h.near, Unprobed: unprobed(e.src)}, StateHit, ""
		}
		state, detail = StateError, describe(err)
		switch {
		case errors.Is(err, ErrRestricted), errors.Is(err, ErrTrial), errors.Is(err, ErrNoAudio):
			state = StateRestricted
			s.log.Info("音源　跳过一首", "音源", e.src.Name(), "原因", reason(err))
		case detail == "超时":
			state = StateTimeout
			s.record(e, searchTimeout, err)
		default:
			s.log.Warn("音源　取不到音频", "音源", e.src.Name(), "类型", detail)
		}
	}
	return nil, state, detail
}

// search and address ask a source. What it answers is not ours to trust: a fault while
// reading it is that source's error.
func (s *Service) search(ctx context.Context, src Source, q string) (c []Candidate, err error) {
	defer s.fault(&err)
	return src.Search(ctx, q)
}

func (s *Service) address(ctx context.Context, src Source, c Candidate) (u string, err error) {
	defer s.fault(&err)
	if _, set := ctx.Value(qualityKey{}).(Quality); !set {
		ctx = context.WithValue(ctx, qualityKey{}, s.Quality())
	}
	return src.Resolve(ctx, c)
}

// Quality is the preferred online audio tier. The selected tier is tried first,
// then every lower tier in order until a playable stream is found.
type Quality string

const (
	HiRes  Quality = "hires"  // FLAC, no bitrate cap (target: 24-bit Hi-Res)
	MP3320 Quality = "mp3320"
	MP3192 Quality = "mp3192"
	MP3128 Quality = "mp3128"
)

var qualityOrder = []Quality{HiRes, MP3320, MP3192, MP3128}

// qualityFalls returns the selected tier and every lower tier.
func qualityFalls(q Quality) []Quality {
	start := 0
	for i, v := range qualityOrder {
		if v == q { start = i; break }
	}
	return append([]Quality(nil), qualityOrder[start:]...)
}

type qualityKey struct{}

// QualityOf is the quality a Resolve is asked for.
func QualityOf(ctx context.Context) Quality {
	if q, ok := ctx.Value(qualityKey{}).(Quality); ok {
		return q
	}
	return HiRes
}

// Quality is the quality asked for first; unavailable tiers fall through one level at a time.
func (s *Service) Quality() Quality {
	if q, ok := s.quality.Load().(Quality); ok {
		return q
	}
	return HiRes
}

// SetQuality changes it; false for a value that is not one.
func (s *Service) SetQuality(q Quality) bool {
	if !slices.Contains(qualityOrder, q) {
		return false
	}
	s.quality.Store(q)
	return true
}

func (s *Service) fault(err *error) {
	if p := recover(); p != nil {
		s.log.Error("在线　程序出错", "原因", fmt.Sprint(p), "位置", string(debug.Stack()))
		*err = errors.New("返回的内容读不懂")
	}
}

func (s *Service) resolveOne(ctx context.Context, src Source, c Candidate) (st Stream, err error) {
	defer func() {
		if err == nil {
			format := strings.ToLower(st.Type)
			switch {
			case format == string(HiRes) || strings.Contains(format, "flac"):
				format = "flac"
			case strings.HasPrefix(format, "mp3") || strings.Contains(format, "mpeg"):
				format = "mp3"
			case strings.Contains(format, "ogg"):
				format = "ogg"
			case strings.Contains(format, "mp4") || strings.Contains(format, "m4a"):
				format = "m4a"
			}
			s.noteTech(c.Source+":"+c.ID, Tech{Format: format, Kbps: st.Kbps, SampleRate: st.SampleRate, Bits: st.Bits})
		}
	}()
	st, err = s.tryOne(ctx, src, c)
	free, ok := src.(interface {
		ResolveFree(context.Context, Candidate) (string, error)
	})
	var he httpError
	if !ok || ctx.Err() != nil || !errors.Is(err, ErrNoAudio) && !errors.Is(err, ErrRestricted) && !errors.As(err, &he) {
		return st, err
	}
	again, aerr := s.tryOne(ctx, freely{src, free.ResolveFree}, c)
	if aerr != nil {
		return Stream{}, err
	}
	return again, nil
}

// Tech is what a stream really is, read from its audio.
type Tech struct {
	Format     string `json:"format"`
	Kbps       int    `json:"kbps,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Bits       int    `json:"bits,omitempty"`
}

const maxTech = 5000

func (s *Service) noteTech(key string, t Tech) {
	if t.Kbps == 0 && t.Bits == 0 && t.SampleRate == 0 {
		return
	}
	s.techMu.Lock()
	defer s.techMu.Unlock()
	if s.tech == nil {
		s.tech = map[string]Tech{}
	}
	if len(s.tech) >= maxTech {
		clear(s.tech) // rebuilt as songs play; a song not played lately is probed again when asked
	}
	s.tech[key] = t
}

// TechOf is what the song last played as, when it was played since the start.
func (s *Service) TechOf(key string) (Tech, bool) {
	s.techMu.Lock()
	defer s.techMu.Unlock()
	t, ok := s.tech[key]
	return t, ok
}

// freely is a source asked without its session.
type freely struct {
	Source
	resolve func(context.Context, Candidate) (string, error)
}

func (f freely) Resolve(ctx context.Context, c Candidate) (string, error) { return f.resolve(ctx, c) }

// unprobed reports that a source's addresses are taken as they are, without being read.
func unprobed(src Source) bool {
	v, ok := src.(interface{ Unprobed() bool })
	return ok && v.Unprobed()
}

// tryOne takes a candidate's address from its source and, unless the source vouches for its
// own bytes, probes it.
func (s *Service) tryOne(ctx context.Context, src Source, c Candidate) (Stream, error) {
	// LX handles the entire quality ladder inside lxCheck. Do not wrap it in
	// another qualityFalls loop, or every lower tier is requested repeatedly.
	if unprobed(src) {
		if _, ok := src.(interface {
			ResolveAt(context.Context, Candidate, string) (string, error)
		}); ok {
			// Carry the user's selected starting tier into LX's own fallback ladder.
			// Without this, QualityOf(ctx) falls back to HiRes and ignores MP3 128/192.
			lctx := context.WithValue(ctx, qualityKey{}, s.Quality())
			return s.lxCheck(lctx, src, c)
		}
	}
	var last Stream
	var lastErr error
	for _, q := range qualityFalls(s.Quality()) {
		st, err := s.tryAt(ctx, src, c, q)
		if err == nil {
			return st, nil
		}
		last, lastErr = st, err
		var he httpError
		if ctx.Err() != nil || errors.Is(err, ErrTrial) ||
			!errors.Is(err, ErrNoAudio) && !errors.Is(err, ErrRestricted) && !errors.As(err, &he) {
			return last, lastErr
		}
	}
	return last, lastErr
}

func (s *Service) tryAt(ctx context.Context, src Source, c Candidate, q Quality) (Stream, error) {
	u, err := s.address(context.WithValue(ctx, qualityKey{}, q), src, c)
	if err != nil {
		return Stream{}, err
	}
	if unprobed(src) {
		// The source does not vouch for its bytes, so they are read here: a stream that is
		// no audio is asked for again at another rate rather than sent to the speaker.
		if _, ok := src.(interface {
			ResolveAt(context.Context, Candidate, string) (string, error)
		}); ok {
			return s.lxCheck(context.WithValue(ctx, qualityKey{}, q), src, c)
		}
		// The listing's own length: every platform claims one, and it need not hold.
		return Stream{URL: u, Duration: c.Duration}, nil
	}
	st, err := s.check(ctx, s.fetcher(src), u, c.Duration)
	if err != nil {
		return st, err
	}
	if !qualitySatisfied(q, st) {
		return Stream{}, ErrNoAudio
	}
	return st, nil
}

func qualitySatisfied(q Quality, st Stream) bool {
	switch q {
	case HiRes:
		// Prefer 24-bit FLAC, but any playable FLAC is still better than MP3.
		return st.Type == "flac"
	case MP3320:
		return st.Type == "mp3" && st.Kbps >= 310
	case MP3192:
		return st.Type == "mp3" && st.Kbps >= 182
	case MP3128:
		return st.Type == "mp3" && st.Kbps >= 120
	default:
		return false
	}
}

// lxRate is one Subsonic stream request in the quality ladder.
type lxRate struct {
	quality Quality
	bitrate string
}

// lxRates returns every requested quality from the selected tier down to MP3 128.
func lxRates(q Quality) []lxRate {
	var out []lxRate
	for _, tier := range qualityFalls(q) {
		switch tier {
		case HiRes:
			out = append(out, lxRate{quality: tier, bitrate: "0"})
		case MP3320:
			out = append(out, lxRate{quality: tier, bitrate: "320"})
		case MP3192:
			out = append(out, lxRate{quality: tier, bitrate: "192"})
		case MP3128:
			out = append(out, lxRate{quality: tier, bitrate: "128"})
		}
	}
	return out
}

// lxCheck proves a 洛雪 stream holds audio before the speaker is sent to it. If the
// requested tier is unavailable or returns a non-audio response, it tries the next lower tier.
func (s *Service) lxCheck(ctx context.Context, src Source, c Candidate) (Stream, error) {
	at, ok := src.(interface {
		ResolveAt(context.Context, Candidate, string) (string, error)
	})
	if !ok {
		return Stream{}, errors.New("这个音源不能按码率取音频")
	}
	client := s.fetcher(src)
	var lastErr error
	for _, rate := range lxRates(QualityOf(ctx)) {
		rctx := context.WithValue(ctx, qualityKey{}, rate.quality)
		u, err := at.ResolveAt(rctx, c, rate.bitrate)
		if err != nil {
			lastErr = err
			continue
		}
		if ctx.Err() != nil {
			return Stream{}, ctx.Err()
		}
		format, kbps, sampleRate, bits, playable, err := s.plays(ctx, client, u)
		if err != nil {
			lastErr = err
			continue
		}
		switch rate.quality {
		case HiRes:
			// A 16-bit FLAC is the next-best fallback; never skip it for MP3.
			playable = playable && format == "flac"
		case MP3320:
			playable = playable && format == "mp3" && kbps >= 310
		case MP3192:
			playable = playable && format == "mp3" && kbps >= 182
		case MP3128:
			playable = playable && format == "mp3" && kbps >= 120
		}
		if playable {
			return Stream{URL: u, Duration: c.Duration, Type: format, Kbps: kbps, SampleRate: sampleRate, Bits: bits}, nil
		}
		lastErr = ErrNoAudio
	}
	if lastErr == nil {
		lastErr = ErrNoAudio
	}
	return Stream{}, lastErr
}

// plays reports whether an address hands out audio the speaker can play. Only the first bytes
// are read: a recognised container marker is what tells a song from the encrypted file a
// platform sometimes answers with, which has no marker at all. A length is not insisted on —
// the listing's own is what the run goes by, as for any unread stream.
func (s *Service) plays(ctx context.Context, client *http.Client, u string) (format string, kbps, sampleRate, bits int, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", 0, 0, 0, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", 0, 0, 0, false, httpError(resp.StatusCode)
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if !audioHead(head) {
		return "", 0, 0, 0, false, nil
	}
	if n := audio.ID3Size(head); n > 0 && int(n) < len(head) {
		head = head[n:]
	}
	if len(head) >= 4 && string(head[:4]) == "fLaC" {
		meta, _ := audio.ReadMeta(bytes.NewReader(head), ".flac", int64(len(head)))
		return "flac", 0, meta.SampleRate, meta.Bits, true, nil
	}
	if info := audio.MP3Info(head, int64(len(head))); info.Kbps > 0 {
		return "mp3", info.Kbps, info.SampleRate, info.Bits, true, nil
	}
	return "", 0, 0, 0, false, nil
}

// check probes an address: it must answer with audio about as long as the song.
func (s *Service) check(ctx context.Context, c *http.Client, u string, want time.Duration) (Stream, error) {
	body, total, ctype, err := s.fetch(ctx, c, u, 0)
	if err != nil {
		return Stream{}, err
	}
	off := int64(0)
	if n := audio.ID3Size(body); n > 0 {
		off = n
		if n+4096 > int64(len(body)) { // a big cover picture before the audio
			if n >= total {
				return Stream{}, ErrNoAudio
			}
			if body, _, _, err = s.fetch(ctx, c, u, n); err != nil {
				return Stream{}, err
			}
		} else {
			body = body[n:]
		}
	}
	info := audio.MP3Info(body, total-off)
	d := info.Duration
	format, bits, sampleRate := "mp3", info.Bits, info.SampleRate
	if d == 0 {
		// FLAC carries its own duration and bit depth in STREAMINFO.
		d = audio.Duration(body, total-off)
		if len(body) >= 4 && string(body[:4]) == "fLaC" {
			format = "flac"
			if meta, e := audio.ReadMeta(bytes.NewReader(body), ".flac", int64(len(body))); e == nil {
				bits, sampleRate = meta.Bits, meta.SampleRate
			}
		} else {
			format = strings.TrimPrefix(ctype, "audio/")
		}
	}
	if d == 0 {
		return Stream{}, fmt.Errorf("%w（不是 MP3，类型 %.24q，%d 字节）", ErrNoAudio, ctype, total)
	}
	if d < shortestSong && (want == 0 || want >= shortestSong) || want > 0 && float64(d) < minFullShare*float64(want) {
		return Stream{}, ErrTrial
	}
	return Stream{URL: u, Duration: d, Type: format, Kbps: info.Kbps, SampleRate: sampleRate, Bits: bits}, nil
}

func (s *Service) fetch(ctx context.Context, c *http.Client, u string, from int64) (b []byte, total int64, ctype string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, "", ErrNoAudio
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, from+32<<10-1))
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()
	ctype = resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 {
		return nil, 0, "", httpError(resp.StatusCode)
	}
	if strings.HasPrefix(ctype, "text/") || strings.Contains(ctype, "json") {
		return nil, 0, "", fmt.Errorf("%w（给的是网页，HTTP %d）", ErrNoAudio, resp.StatusCode)
	}
	switch resp.StatusCode {
	case http.StatusPartialContent:
		_, t, _ := strings.Cut(resp.Header.Get("Content-Range"), "/")
		if _, err := fmt.Sscan(t, &total); err != nil {
			return nil, 0, "", ErrNoAudio
		}
	case http.StatusOK:
		if from > 0 {
			return nil, 0, "", ErrNoAudio
		}
		total = resp.ContentLength
	default:
		return nil, 0, "", httpError(resp.StatusCode)
	}
	b, err = io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	return b, total, ctype, err
}

// Opener returns a function that opens the song's bytes, asking the platform for a fresh
// address when the current one has expired.
func (s *Service) Opener(f *Found) func(ctx context.Context, rng string) (*http.Response, error) {
	var mu sync.Mutex
	u := f.Stream.URL
	var src Source
	for _, e := range s.srcs {
		if e.src.Name() == f.Candidate.Source {
			src = e.src
		}
	}
	return func(ctx context.Context, rng string) (*http.Response, error) {
		for try := 0; ; try++ {
			mu.Lock()
			cur := u
			mu.Unlock()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, cur, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", userAgent)
			if rng != "" {
				req.Header.Set("Range", rng)
			}
			resp, err := s.fetcher(src).Do(req)
			if err == nil && (resp.StatusCode < 400 || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable) {
				return resp, nil
			}
			if err == nil {
				resp.Body.Close()
				err = httpError(resp.StatusCode)
			}
			if try > 0 || ctx.Err() != nil || src == nil {
				return nil, err
			}
			if try == 0 && f.Unprobed {
				// The address was taken without being read, so a refusal by it may be the
				// platform's own file: look further before giving up on the song.
				if alt, ok := s.alternate(ctx, f, u); ok {
					mu.Lock()
					u = alt
					mu.Unlock()
					continue
				}
			}
			rctx, cancel := context.WithTimeout(ctx, totalBudget)
			st, rerr := s.resolveOne(rctx, src, f.Candidate)
			cancel()
			if rerr != nil {
				return nil, rerr
			}
			mu.Lock()
			u = st.URL
			mu.Unlock()
		}
	}
}

type httpError int

func (e httpError) Error() string { return fmt.Sprintf("平台返回 HTTP %d", int(e)) }

// looksAudio reads the first bytes of a whole-track answer and says whether they are an audio
// container. The body is put back together, so a response that does hold audio is returned as
// it came. head is what was read, for a log.
func looksAudio(resp *http.Response) (head []byte, ok bool) {
	if resp.Body == nil {
		return nil, true // nothing to judge: leave it to the speaker
	}
	head, _ = io.ReadAll(io.LimitReader(resp.Body, 3<<10))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	return head, audioHead(head)
}

// audioHead reports a recognised audio container from its first bytes.
func audioHead(b []byte) bool {
	if len(b) >= 4 {
		switch string(b[:4]) {
		case "fLaC", "OggS", "RIFF", "FORM":
			return true
		}
		// An MP4 or M4A box: size, then "ftyp".
		if len(b) >= 12 && string(b[4:8]) == "ftyp" {
			return true
		}
	}
	if len(b) >= 3 && string(b[:3]) == "ID3" {
		return true
	}
	if len(b) >= 2 && b[0] == 0xff && b[1]&0xe0 == 0xe0 {
		return true // an MPEG frame, whether MP3 or ADTS
	}
	return false
}

// alternate asks a song's own source for another address for it, when the one being played was
// not read and the source found more than one form of the song. With none, the original stands.
func (s *Service) alternate(ctx context.Context, f *Found, tried string) (string, bool) {
	var src Source
	for _, e := range s.srcs {
		if e.src.Name() == f.Candidate.Source {
			src = e.src
		}
	}
	if src == nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, totalBudget)
	defer cancel()
	u, err := s.address(ctx, src, f.Candidate)
	if err != nil || u == "" || u == tried {
		return "", false
	}
	s.log.Info("音源　换一个音质再试", "音源", src.Name(), "歌曲", f.Candidate.Title)
	return u, true
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// hosts are the only domains TrackFerry talks to, directly or through redirects.
var hosts = []string{"kuwo.cn", "kugou.com", "migu.cn", "music.163.com", "music.126.net"}

func allowedHost(h string) bool {
	for _, d := range hosts {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// Client returns the HTTP client for the platforms: it only reaches their own domains and
// never a private address, whatever an answer or redirect says.
func Client() *http.Client { return ClientWith(nil) }

// ClientWith is Client that also reaches the hosts in extra. A source on this network, such as
// a self-hosted server, needs its own address let through: only those exact addresses may be
// private, and the platforms' own checks are untouched.
func ClientWith(extra []string) *http.Client {
	allowed := func(h string) bool {
		if allowedHost(h) {
			return true
		}
		return slices.Contains(extra, h)
	}
	d := &net.Dialer{Timeout: 3 * time.Second, Control: func(_, addr string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(addr)
		if err != nil {
			return err
		}
		if ip := ap.Addr().Unmap(); (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) && !slices.Contains(extra, ip.String()) {
			return errors.New("refusing a private address")
		}
		return nil
	}}
	tr := &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second,
		MaxIdleConnsPerHost: 4, IdleConnTimeout: time.Minute, ForceAttemptHTTP2: true}
	return &http.Client{Transport: guard{tr, allowed}, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) > 4 {
			return errors.New("too many redirects")
		}
		return nil
	}}
}

type guard struct {
	next http.RoundTripper
	ok   func(host string) bool
}

func (g guard) RoundTrip(r *http.Request) (*http.Response, error) {
	if !g.ok(r.URL.Hostname()) || r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		return nil, errors.New("host not allowed")
	}
	return g.next.RoundTrip(r)
}

// maxOffered bounds the songs shown that can still be picked.
const maxOffered = 600

// Search lists what the enabled platforms offer for a free text, interleaved, and the
// platforms that did not answer.
func (s *Service) Search(ctx context.Context, q string) ([]Candidate, []Outcome) {
	lists := make([][]Candidate, len(s.srcs))
	outs := make([]Outcome, len(s.srcs))
	var wg sync.WaitGroup
	for i, e := range s.srcs {
		outs[i] = Outcome{Source: e.src.Name(), Candidates: []Candidate{}}
		s.mu.Lock()
		on, open := e.enabled, time.Now().Before(e.open)
		s.mu.Unlock()
		switch {
		case !on:
			outs[i].State = StateDisabled
			continue
		case open:
			outs[i].State, outs[i].Detail = StateOpen, "连续失败，暂停询问一会儿"
			continue
		}
		wg.Go(func() {
			start := time.Now()
			c, done := context.WithTimeout(ctx, 4*time.Second)
			defer done()
			cands, err := s.search(c, e.src, q)
			if ctx.Err() == nil {
				s.record(e, time.Since(start), err)
			}
			outs[i].MS = time.Since(start).Milliseconds()
			switch {
			case err != nil:
				outs[i].State, outs[i].Detail = StateError, describe(err)
			case len(cands) == 0:
				outs[i].State = StateMiss
			default:
				outs[i].State, lists[i] = StateHit, cands
			}
		})
	}
	wg.Wait()
	out := []Candidate{}
	for n := 0; ; n++ {
		more := false
		for _, l := range lists {
			if n < len(l) {
				out, more = append(out, l[n]), true
			}
		}
		if !more || len(out) >= 60 {
			break
		}
	}
	s.offMu.Lock()
	defer s.offMu.Unlock()
	if s.offered == nil {
		s.offered = map[string]Candidate{}
	}
	for i := range out {
		c := &out[i]
		c.Key = c.Source + ":" + c.ID
		if _, ok := s.offered[c.Key]; !ok {
			s.order = append(s.order, c.Key)
		}
		s.offered[c.Key] = *c
	}
	for len(s.order) > maxOffered {
		delete(s.offered, s.order[0])
		s.order = s.order[1:]
	}
	if s.Remember != nil && len(out) > 0 {
		keep := make(map[string]Saved, len(out))
		for _, c := range out {
			keep[c.Key] = savedOf(c)
		}
		go s.Remember(keep)
	}
	return out, outs
}

// Offer makes songs from a server-provided collection available to Take, just like
// songs returned by Search. The key remains source:id so it can be sent to the play API.
func (s *Service) Offer(candidates []Candidate) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	s.offMu.Lock()
	if s.offered == nil { s.offered = map[string]Candidate{} }
	for _, c := range candidates {
		if c.Source == "" || c.ID == "" { continue }
		c.Key = c.Source + ":" + c.ID
		if _, ok := s.offered[c.Key]; !ok { s.order = append(s.order, c.Key) }
		s.offered[c.Key] = c
		out = append(out, c)
	}
	for len(s.order) > maxOffered {
		delete(s.offered, s.order[0])
		s.order = s.order[1:]
	}
	s.offMu.Unlock()
	if s.Remember != nil && len(out) > 0 {
		keep := make(map[string]Saved, len(out))
		for _, c := range out { keep[c.Key] = savedOf(c) }
		go s.Remember(keep)
	}
	return out
}

// OfferTemporary keeps short-lived artist results available to the queue without persisting
// member-backed candidates. Their playback rights are rechecked when each song is taken.
func (s *Service) OfferTemporary(candidates []Candidate) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	s.offMu.Lock()
	if s.offered == nil {
		s.offered = map[string]Candidate{}
	}
	for _, c := range candidates {
		if c.Source == "" || c.ID == "" {
			continue
		}
		c.Key = c.Source + ":" + c.ID
		if _, ok := s.offered[c.Key]; !ok {
			s.order = append(s.order, c.Key)
		}
		s.offered[c.Key] = c
		out = append(out, c)
	}
	for len(s.order) > maxOffered {
		delete(s.offered, s.order[0])
		s.order = s.order[1:]
	}
	s.offMu.Unlock()
	return out
}

func savedOf(c Candidate) Saved {
	return Saved{Source: c.Source, ID: c.ID, Ref: c.ref, Title: c.Title, Artist: c.Artist, ListedMS: c.Duration.Milliseconds(), Album: c.Album, Cover: c.Cover, Lyric: c.lyric}
}

// candidate is a song a search showed: among the last shown, or remembered.
func (s *Service) candidate(key string) (Candidate, bool) {
	s.offMu.Lock()
	c, ok := s.offered[key]
	s.offMu.Unlock()
	if ok || s.Recall == nil {
		return c, ok
	}
	saved, ok := s.Recall(key)
	if !ok || saved.Source+":"+saved.ID != key {
		return Candidate{}, false
	}
	c = saved.Found().Candidate
	c.Key = key
	s.offMu.Lock()
	if s.offered == nil {
		s.offered = map[string]Candidate{}
	}
	if _, had := s.offered[key]; !had {
		s.order = append(s.order, key)
	}
	s.offered[key] = c
	s.offMu.Unlock()
	return c, true
}

// ErrGone is a song that was not shown, or too long ago.
var ErrGone = errors.New("找不到这首在线歌了，请重新搜索")

// Take gets a song ready that Search has shown. The error is for people.
func (s *Service) Take(ctx context.Context, key string) (*Found, error) {
	c, ok := s.candidate(key)
	if !ok {
		return nil, ErrGone
	}
	for _, e := range s.srcs {
		if e.src.Name() != c.Source {
			continue
		}
		s.mu.Lock()
		on := e.enabled
		s.mu.Unlock()
		if !on {
			return nil, errors.New("这个音源关着")
		}
		ctx, cancel := context.WithTimeout(ctx, 2*totalBudget)
		defer cancel()
		st, err := s.resolveOne(ctx, e.src, c)
		if err != nil {
			s.log.Warn("音源　点的歌放不了", "音源", c.Source, "原因", reason(err))
			return nil, errors.New(describe(err))
		}
		return &Found{Candidate: c, Stream: st, Unprobed: unprobed(e.src)}, nil
	}
	return nil, ErrGone
}

// HTTPClient is a source that needs its own client: one on this network must reach a private
// address the platforms' client refuses. Sources that do not implement it use the shared one.
type HTTPClient interface{ HTTP() *http.Client }

// fetcher returns the client that may reach src's audio.
func (s *Service) fetcher(src Source) *http.Client {
	if h, ok := src.(HTTPClient); ok {
		if c := h.HTTP(); c != nil {
			return c
		}
	}
	return s.probe
}

// FindArtistCandidates searches LXServer for a singer and keeps the results available
// to the playback queue without persisting member-backed candidates.
func (s *Service) FindArtistCandidates(ctx context.Context, artist string, limit int) ([]Candidate, []Outcome) {
	var e *entry
	var lx *Lx
	s.mu.Lock()
	for _, candidate := range s.srcs {
		if candidate.src.Name() == "lx" {
			e = candidate
			lx, _ = candidate.src.(*Lx)
			break
		}
	}
	enabled, open := false, false
	if e != nil {
		enabled, open = e.enabled, time.Now().Before(e.open)
	}
	s.mu.Unlock()

	out := Outcome{Source: "lx", Candidates: []Candidate{}}
	switch {
	case e == nil || lx == nil || !enabled:
		out.State, out.Detail = StateDisabled, "LX 音源未启用"
		return nil, []Outcome{out}
	case open:
		out.State, out.Detail = StateOpen, "连续失败，暂停询问一会儿"
		return nil, []Outcome{out}
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, totalBudget)
	defer cancel()
	searchCtx, stop := context.WithTimeout(ctx, searchTimeout)
	candidates, err := lx.ArtistSongs(searchCtx, artist, limit)
	stop()
	if err != nil {
		s.record(e, time.Since(start), err)
		out.State, out.Detail = StateError, describe(err)
		return nil, []Outcome{out}
	}
	if len(candidates) == 0 {
		s.record(e, time.Since(start), nil)
		out.State, out.Detail = StateMiss, "LX 没找到这位歌手的歌曲"
		return nil, []Outcome{out}
	}

	candidates = s.OfferTemporary(candidates)
	s.record(e, time.Since(start), nil)
	out.Candidates = slices.Clone(candidates[:min(shownPerSource, len(candidates))])
	out.State = StateHit
	out.Detail = fmt.Sprintf("LX 找到这位歌手的 %d 首在线歌曲", len(candidates))
	return candidates, []Outcome{out}
}

// FindArtist finds and resolves the first playable LX song for a singer. It also returns the
// complete temporary candidate list so the caller can build a continuous queue.
func (s *Service) FindArtist(ctx context.Context, artist string, limit int) (*Found, []Candidate, []Outcome) {
	candidates, outs := s.FindArtistCandidates(ctx, artist, limit)
	if len(candidates) == 0 {
		return nil, candidates, outs
	}
	ctx, cancel := context.WithTimeout(ctx, 2*totalBudget)
	defer cancel()
	var lastErr error
	for _, c := range candidates[:min(maxPerSource, len(candidates))] {
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		found, err := s.Take(ctx, c.Key)
		if err == nil {
			if len(outs) > 0 {
				outs[0].Detail = fmt.Sprintf("LX 找到 %d 首歌，已取得第一首可播放音频", len(candidates))
			}
			return found, candidates, outs
		}
		lastErr = err
	}
	if lastErr != nil {
		var lxEntry *entry
		s.mu.Lock()
		for _, e := range s.srcs {
			if e.src.Name() == "lx" {
				lxEntry = e
				break
			}
		}
		s.mu.Unlock()
		if lxEntry != nil {
			s.record(lxEntry, 0, lastErr)
		}
		if len(outs) > 0 {
			outs[0].State, outs[0].Detail = StateError, lastErr.Error()
			if lastErr.Error() == ErrRestricted.Error() || lastErr.Error() == ErrTrial.Error() || lastErr.Error() == ErrNoAudio.Error() {
				outs[0].State = StateRestricted
			}
		}
	}
	return nil, candidates, outs
}

func (s *Service) Lx() *Lx {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.srcs {
		if l, ok := e.src.(*Lx); ok {
			return l
		}
	}
	return nil
}

// ClientForServer is the client for a source on this network, such as a self-hosted 洛雪
// server. Besides reaching that address, it follows the server's redirect to a platform's own
// address: the server does not relay online songs itself, it answers the stream request with
// where the song is while its cache is cold. Only this client is that permissive.
func ClientForServer(base string) *http.Client {

	c := ClientWith(LxHosts(base))
	cp := *c
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 10 {
			return errors.New("too many redirects")
		}
		// The address this follows carries the server's password in its query. Go would send it
		// on as the Referer of the request to the platform's CDN, which is a third party: the
		// password would go with it. Dropped here, and the new address keeps nothing of it.
		req.Header.Del("Referer")
		return nil
	}
	if g, ok := cp.Transport.(guard); ok {
		g.ok = func(string) bool { return true } // the server sends us where the song is
		cp.Transport = g
	}
	return &cp
}

// SetOrder puts the named platforms first, in this order, and the rest after them. Names that
// are unknown are ignored, so a stale saved order never hides a platform.
func (s *Service) SetOrder(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first, rest []*entry
	for _, n := range names {
		for _, e := range s.srcs {
			if e.src.Name() == n && !slices.Contains(first, e) {
				first = append(first, e)
			}
		}
	}
	for _, e := range s.srcs {
		if !slices.Contains(first, e) {
			rest = append(rest, e)
		}
	}
	s.srcs = append(first, rest...)
}

// SetLx installs (or with nil removes) the self-hosted 洛雪 source, keeping its place in the
// order and its switch. The page changes this without a restart.
func (s *Service) SetLx(src Source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.srcs {
		if e.src.Name() != "lx" {
			continue
		}
		if src == nil {
			s.srcs = append(s.srcs[:i], s.srcs[i+1:]...)
			return
		}
		s.srcs[i].src = src
		return
	}
	if src != nil {
		s.srcs = append(s.srcs, &entry{src: src, enabled: true, h: Health{Source: src.Name()}})
	}
}
