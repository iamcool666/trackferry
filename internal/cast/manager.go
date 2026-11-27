package cast

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"

	"trackferry/internal/logx"
	"trackferry/internal/tv"
)

const (
	settingOn  = "cast.on" // the devices turned on, as JSON
	scanEvery  = 5 * time.Minute
	scanWait   = 3 * time.Second
	goneAfter  = 16 * time.Minute // not heard in a scan for this long: no longer on the network
	restartGap = 5 * time.Second
)

// Finder looks for devices on the network.
type Finder func(ctx context.Context) ([]Device, error)

// Manager finds devices, keeps which ones the user turned on, and drives those.
type Manager struct {
	Remote     *tv.Remote
	Prepare    Prepare
	Setting    func(ctx context.Context, key string) (string, error)
	SetSetting func(ctx context.Context, key, value string) error
	Log        *slog.Logger
	// Optional; the defaults search the network and talk DLNA and Cast.
	Finders []Finder
	Open    func(Device) Renderer

	mu       sync.Mutex
	found    map[string]Device
	on       map[string]Device
	players  map[string]context.CancelFunc
	scanning bool
	lastScan time.Time
	scanErr  string
	poke     chan struct{}
	ctx      context.Context
}

// View is a device as the settings page shows it.
type View struct {
	Device
	On      bool `json:"on"`
	Present bool `json:"present"` // heard in a recent scan
	Online  bool `json:"online"`  // turned on and answering
}

// Listing is the list and the state of searching.
type Listing struct {
	Devices  []View `json:"devices"`
	Scanning bool   `json:"scanning"`
	LastMS   int64  `json:"last_ms,omitempty"`
	Error    string `json:"error,omitempty"`
}

type saved struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Model    string `json:"model,omitempty"`
	Location string `json:"location"`
}

// Run searches now and then, and drives the devices that are on, until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	m.found, m.on, m.players = map[string]Device{}, map[string]Device{}, map[string]context.CancelFunc{}
	m.poke = make(chan struct{}, 1)
	m.mu.Unlock()
	if raw, err := m.Setting(ctx, settingOn); err == nil && raw != "" {
		var list []saved
		if json.Unmarshal([]byte(raw), &list) == nil {
			m.mu.Lock()
			for _, s := range list {
				d := Device{ID: s.ID, Name: s.Name, Kind: s.Kind, Model: s.Model, Location: s.Location}
				m.on[d.ID] = d
				m.startLocked(d)
			}
			m.mu.Unlock()
		}
	}
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.poke:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
		}
		m.scan(ctx)
		t.Reset(scanEvery)
	}
}

// Rescan asks for a search now.
func (m *Manager) Rescan() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.poke == nil {
		return
	}
	m.scanning = true
	select {
	case m.poke <- struct{}{}:
	default:
	}
}

func (m *Manager) finders() []Finder {
	if m.Finders != nil {
		return m.Finders
	}
	client := &http.Client{Timeout: 4 * time.Second}
	return []Finder{
		func(ctx context.Context) ([]Device, error) {
			locs, err := searchDLNA(ctx, scanWait)
			if err != nil {
				return nil, err
			}
			var out []Device
			for _, l := range locs {
				d, ok, err := describeDLNA(ctx, client, l)
				if err == nil && ok {
					out = append(out, d.Device)
				}
			}
			return out, nil
		},
		findChromecasts,
	}
}

func findChromecasts(ctx context.Context) ([]Device, error) {
	r, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}
	entries := make(chan *zeroconf.ServiceEntry, 32)
	bctx, cancel := context.WithTimeout(ctx, scanWait)
	defer cancel()
	if err := r.Browse(bctx, "_googlecast._tcp", "local.", entries); err != nil {
		return nil, err
	}
	var out []Device
	seen := map[string]bool{}
	for e := range entries {
		if len(e.AddrIPv4) == 0 || e.Port == 0 {
			continue
		}
		addr := net.JoinHostPort(e.AddrIPv4[0].String(), strconv.Itoa(e.Port))
		d := Device{ID: castID(e.Text, addr), Kind: Chromecast, Location: addr}
		for _, t := range e.Text {
			t = unescapeTXT(t)
			if v, ok := strings.CutPrefix(t, "fn="); ok {
				d.Name = v
			} else if v, ok := strings.CutPrefix(t, "md="); ok {
				d.Model = v
			}
		}
		if d.Name == "" {
			d.Name = unescapeTXT(e.Instance)
		}
		if !seen[d.ID] {
			seen[d.ID] = true
			out = append(out, d)
		}
	}
	return out, nil
}

// unescapeTXT undoes the DNS text form the resolver hands out: a byte past ASCII comes as
// \DDD (so a Chinese name reads "\229\174\162…"), and a quote or backslash as \" and \\.
func unescapeTXT(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b = append(b, c)
			continue
		}
		if i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
			if n, err := strconv.Atoi(s[i+1 : i+4]); err == nil && n < 256 {
				b = append(b, byte(n))
				i += 3
				continue
			}
		}
		b = append(b, s[i+1])
		i++
	}
	return string(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (m *Manager) scan(ctx context.Context) {
	defer logx.Recover(m.Log, "投放")
	m.mu.Lock()
	m.scanning = true
	m.mu.Unlock()
	var (
		wg   sync.WaitGroup
		lock sync.Mutex
		all  []Device
		errs []string
	)
	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, f := range m.finders() {
		wg.Add(1)
		go func(f Finder) {
			defer wg.Done()
			got, err := f(sctx)
			lock.Lock()
			defer lock.Unlock()
			all = append(all, got...)
			if err != nil {
				errs = append(errs, err.Error())
			}
		}(f)
	}
	wg.Wait()
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scanning, m.lastScan = false, now
	m.scanErr = ""
	if len(all) == 0 && len(errs) > 0 {
		m.scanErr = "搜索没能进行：" + errs[0]
	}
	for _, d := range all {
		d.seen = now
		old, had := m.on[d.ID]
		m.found[d.ID] = d
		if had && old.Location != d.Location {
			// It moved (a new address from the router): drive it where it is now.
			m.Log.Info("投放　设备换了地址", "设备", d.Name)
			m.on[d.ID] = d
			m.stopLocked(d.ID)
			m.startLocked(d)
			m.saveLocked()
		}
	}
	for id, d := range m.found {
		if now.Sub(d.seen) > goneAfter {
			delete(m.found, id)
		}
	}
}

func (m *Manager) open(d Device) Renderer {
	if m.Open != nil {
		return m.Open(d)
	}
	if d.Kind == Chromecast {
		return newChromecast(d.Location)
	}
	return newDLNA(&http.Client{Timeout: 6 * time.Second}, d.Location)
}

func (m *Manager) startLocked(d Device) {
	if m.ctx == nil || m.players[d.ID] != nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.players[d.ID] = cancel
	p := newPlayer(d.ID, d.Name, m.open(d), m.Remote, m.Prepare, m.Log)
	go func() {
		// A player that fails for any reason starts again, so a device stays usable.
		for ctx.Err() == nil {
			func() {
				defer logx.Recover(m.Log, "投放")
				p.run(ctx)
			}()
			select {
			case <-ctx.Done():
			case <-time.After(restartGap):
				p = newPlayer(d.ID, d.Name, m.open(d), m.Remote, m.Prepare, m.Log)
			}
		}
	}()
}

func (m *Manager) stopLocked(id string) {
	if cancel := m.players[id]; cancel != nil {
		cancel()
		delete(m.players, id)
	}
	if m.Remote != nil {
		m.Remote.Revoke(id)
	}
}

func (m *Manager) saveLocked() {
	list := make([]saved, 0, len(m.on))
	for _, d := range m.on {
		list = append(list, saved{d.ID, d.Name, d.Kind, d.Model, d.Location})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	b, _ := json.Marshal(list)
	if m.SetSetting != nil {
		if err := m.SetSetting(context.Background(), settingOn, string(b)); err != nil {
			m.Log.Warn("投放　保存设置失败", "原因", err.Error())
		}
	}
}

// ErrNoDevice means the device was not found on the network.
var ErrNoDevice = errors.New("网络上没找到这台设备，先搜索一次")

// Set turns a device on or off.
func (m *Manager) Set(id string, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return errors.New("投放还没准备好")
	}
	if !on {
		if _, ok := m.on[id]; !ok {
			return nil
		}
		delete(m.on, id)
		m.stopLocked(id)
		m.saveLocked()
		return nil
	}
	d, ok := m.found[id]
	if !ok {
		if d, ok = m.on[id]; ok {
			return nil
		}
		return ErrNoDevice
	}
	m.on[id] = d
	m.startLocked(d)
	m.saveLocked()
	return nil
}

// On returns the devices turned on, for the list of outputs.
func (m *Manager) On() []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Device, 0, len(m.on))
	for _, d := range m.on {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Status lists every device known.
func (m *Manager) Listing() Listing {
	m.mu.Lock()
	defer m.mu.Unlock()
	views := map[string]View{}
	for id, d := range m.found {
		views[id] = View{Device: d, Present: true}
	}
	for id, d := range m.on {
		v, ok := views[id]
		if !ok {
			v = View{Device: d}
		}
		v.On = true
		if m.Remote != nil {
			_, v.Online = m.Remote.State(id, false)
		}
		views[id] = v
	}
	out := Listing{Devices: make([]View, 0, len(views)), Scanning: m.scanning, Error: m.scanErr}
	if !m.lastScan.IsZero() {
		out.LastMS = m.lastScan.UnixMilli()
	}
	for _, v := range views {
		out.Devices = append(out.Devices, v)
	}
	sort.Slice(out.Devices, func(i, j int) bool {
		if out.Devices[i].On != out.Devices[j].On {
			return out.Devices[i].On
		}
		return out.Devices[i].Name < out.Devices[j].Name
	})
	return out
}

// Is reports whether id names a cast device.
func Is(id string) bool { return strings.HasPrefix(id, "dlna-") || strings.HasPrefix(id, "cast-") }
