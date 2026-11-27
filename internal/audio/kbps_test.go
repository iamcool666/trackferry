package audio

import "testing"

// MPEG-1 Layer III frames at 128 kbps, 44.1 kHz: the rate comes from the frames themselves.
func TestMP3Kbps(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	var b []byte
	for range 60 {
		b = append(b, frame...)
	}
	m := MP3Info(b, int64(len(b)))
	if m.Kbps != 128 || m.SampleRate != 44100 {
		t.Fatalf("kbps %d rate %d", m.Kbps, m.SampleRate)
	}
}
