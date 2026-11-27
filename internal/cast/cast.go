// Package cast plays TrackFerry's queues on the players of the home network that take an
// address and play it: DLNA (UPnP) renderers and Chromecast devices. Each one the user turns on
// is driven from here as if it were a TrackFerry TV, so every screen that moves, controls or
// swaps songs on a TV does the same with it.
package cast

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Kinds of devices.
const (
	DLNA       = "dlna"
	Chromecast = "chromecast"
)

// Media is one song as a device fetches it.
type Media struct {
	URL      string
	Type     string // audio/mpeg, audio/flac, …
	Title    string
	Artist   string
	Album    string
	Duration time.Duration // 0 when unknown
}

// States a device reports.
const (
	Playing   = "playing"
	Paused    = "paused"
	Buffering = "buffering"
	Stopped   = "stopped"  // nothing playing: never started, stopped, or the song is over
	Finished  = "finished" // the device says the song played to its end
	Failed    = "error"
)

// Status is what a device says it does, read from it each time.
type Status struct {
	State    string
	Position time.Duration
	Duration time.Duration // 0 when the device does not say
	Volume   int           // 0–100; -1 when it does not say
	URL      string        // what it has loaded, when it says
}

// Renderer is one device that can be told to play an address.
type Renderer interface {
	Load(ctx context.Context, m Media, start time.Duration) error
	Play(ctx context.Context) error
	Pause(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, to time.Duration) error
	SetVolume(ctx context.Context, v int) error
	Status(ctx context.Context) (Status, error)
	Close()
}

// Device is a player found on the network.
type Device struct {
	ID    string `json:"id"` // dlna-… or cast-…, safe in a URL
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Model string `json:"model,omitempty"`

	// Where to reach it: a DLNA device's description, a Chromecast's host:port.
	Location string `json:"-"`
	seen     time.Time
}

// ErrUnsupported means the device does not offer what was asked.
var ErrUnsupported = errors.New("这台设备不支持这个操作")

// safeID keeps letters, digits and dashes, so an ID can stand in a path.
func safeID(prefix, raw string) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == ':' || r == '_':
			b.WriteByte('-')
		}
		if b.Len() >= 72 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}
