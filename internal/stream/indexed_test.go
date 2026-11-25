package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeFLAC is frames of 4096 samples at 44.1 kHz whose sound is zeroes, with a header in
// between that belongs to no frame.
func fakeFLAC(frames int) (file []byte, sizes []int) {
	info := make([]byte, 34)
	binary.BigEndian.PutUint16(info, 4096)
	binary.BigEndian.PutUint16(info[2:], 4096)
	info[10], info[11], info[12], info[13] = 44100>>12, 44100>>4&255, 44100&15<<4|1<<1, 15<<4
	binary.BigEndian.PutUint32(info[14:], uint32(frames*4096-96))
	file = append([]byte("fLaC\x00\x00\x00\x22"), info...)
	file = append(file, 0x81, 0, 0, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	header := func(n int) []byte {
		h := []byte{0xFF, 0xF8, 0xC9, 0x18}
		if n < 0x80 {
			h = append(h, byte(n))
		} else {
			h = append(h, 0xC0|byte(n>>6), 0x80|byte(n&63))
		}
		return append(h, crc8(h))
	}
	for i := range frames {
		f := append(header(i), make([]byte, 900+i%70)...)
		if i < 400 {
			copy(f[300:], header(500))
		}
		file, sizes = append(file, f...), append(sizes, len(f))
	}
	return file, sizes
}

func TestFLACForABrowserHasAnIndex(t *testing.T) {
	dir := t.TempDir()
	b, sizes := fakeFLAC(1500)
	os.WriteFile(filepath.Join(dir, "a.flac"), b, 0o600)
	s := New("", dir)
	get := func(rng string) (*httptest.ResponseRecorder, []byte) {
		r := httptest.NewRequest("GET", "/", nil)
		if rng != "" {
			r.Header.Set("Range", rng)
		}
		w := httptest.NewRecorder()
		if err := s.SongHere(w, r, "a.flac", "a", true); err != nil {
			t.Fatal(err)
		}
		x, _ := io.ReadAll(w.Result().Body)
		return w, x
	}
	w, x := get("")
	if w.Code != 200 || w.Header().Get("Content-Type") != "audio/mp4" || string(x[4:8]) != "ftyp" {
		t.Fatal(w.Code, w.Header())
	}
	at := bytes.Index(x, []byte("stsz"))
	if n := binary.BigEndian.Uint32(x[at+12:]); int(n) != len(sizes) {
		t.Fatal(n, "frames")
	}
	for i, n := range sizes {
		if got := binary.BigEndian.Uint32(x[at+16+4*i:]); int(got) != n {
			t.Fatal("frame", i, got, n)
		}
	}
	at = bytes.Index(x, []byte("stts"))
	if v := x[at+8 : at+28]; !bytes.Equal(v, be(2, 1499, 4096, 1, 4000)) {
		t.Fatal(v)
	}
	at = bytes.Index(x, []byte("stco"))
	off := binary.BigEndian.Uint32(x[at+12:])
	if !bytes.Equal(x[off:], b[42+14:]) {
		t.Fatal("not the same frames")
	}
	if w, y := get("bytes=100-99999"); w.Code != 206 || !bytes.Equal(y, x[100:100000]) {
		t.Fatal(w.Code, len(y))
	}
	// A changed file is read anew, one that cannot be indexed stays as it is.
	b, _ = fakeFLAC(200)
	os.WriteFile(filepath.Join(dir, "a.flac"), b, 0o600)
	if _, x = get(""); !bytes.HasSuffix(x, b[42+14:]) || string(x[4:8]) != "ftyp" {
		t.Fatal("the old file")
	}
	b = b[:len(b)-5000]
	os.WriteFile(filepath.Join(dir, "a.flac"), b, 0o600)
	if _, x = get(""); !bytes.Equal(x, b) {
		t.Fatal("half a file was indexed")
	}
}

func TestResumesAWAVAtATime(t *testing.T) {
	dir := t.TempDir()
	fmtc := append([]byte("fmt \x10\x00\x00\x00\x01\x00\x02\x00"), 0x44, 0xAC, 0, 0, 0x10, 0xB1, 2, 0, 4, 0, 16, 0)
	data := make([]byte, 176400*60)
	for i := range data {
		data[i] = byte(i / 176400)
	}
	b := append([]byte("RIFF\x00\x00\x00\x00WAVE"), append([]byte("LIST\x03\x00\x00\x00abc\x00"), fmtc...)...)
	b = binary.LittleEndian.AppendUint32(append(b, "data"...), uint32(len(data)))
	b = append(append(b, data...), "id3 tags"...)
	os.WriteFile(filepath.Join(dir, "a.wav"), b, 0o600)
	s := New("http://nas", dir)
	h, err := s.Song("a.wav", "a", time.Minute)
	if err != nil || !h.Seekable() {
		t.Fatal(err)
	}
	n, at, err := h.Resume(context.Background(), 20500*time.Millisecond)
	if err != nil || at != 20500*time.Millisecond {
		t.Fatal(at, err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", n.URL[len("http://nas"):], nil))
	x := w.Body.Bytes()
	if string(x[:4]) != "RIFF" || string(x[12:16]) != "fmt " || string(x[36:40]) != "data" || binary.LittleEndian.Uint32(x[40:]) != 176400*79/2 || x[44] != 20 || x[44+88200] != 21 {
		t.Fatal(x[:48])
	}
}
