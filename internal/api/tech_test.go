package api

import (
	"testing"

	"trackferry/internal/online"
)

func TestTechLabels(t *testing.T) {
	for _, c := range []struct {
		in          online.Tech
		tier, label string
	}{
		{online.Tech{Format: "flac", Bits: 24, SampleRate: 96000}, "hires", "FLAC 24bit 96kHz"},
		{online.Tech{Format: "flac", Bits: 16, SampleRate: 44100}, "lossless", "FLAC 16bit 44.1kHz"},
		{online.Tech{Format: "mp3", Kbps: 320}, "high", "MP3 320k"},
		{online.Tech{Format: "mp3", Kbps: 128}, "standard", "MP3 128k"},
		{online.Tech{Format: "mp3"}, "", "MP3"},
	} {
		if v := viewOf(c.in, true); v.Tier != c.tier || v.Label != c.label {
			t.Errorf("%+v: %s %q", c.in, v.Tier, v.Label)
		}
	}
}
