package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrNoSeek means a stream cannot be started from its middle.
var ErrNoSeek = errors.New("this format cannot start in the middle")

// Served returns what the speaker took so far.
func (h *Handle) Served() Served {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.served
}

// Lost reports that the speaker hung up before it had the whole stream.
func (h *Handle) Lost() bool {
	s := h.Served()
	return s.Requests > 0 && !s.Open && s.Total > 0 && s.Bytes < s.Total
}

// source reads a song's bytes from an offset; end < 0 reads to its end.
func (it *item) source(ctx context.Context, from, end int64) (io.ReadCloser, int64, error) {
	if it.open == nil {
		f, err := os.Open(it.path)
		if err != nil {
			return nil, 0, err
		}
		fi, err := f.Stat()
		if err == nil {
			_, err = f.Seek(from, io.SeekStart)
		}
		if err != nil {
			f.Close()
			return nil, 0, err
		}
		return f, fi.Size(), nil
	}
	rng := fmt.Sprintf("bytes=%d-", from)
	if end >= 0 {
		rng += strconv.FormatInt(end, 10)
	}
	resp, err := it.open(ctx, rng)
	if err != nil {
		return nil, 0, err
	}
	var a, b, total int64
	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, 0, ErrNoSeek
	}
	if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &a, &b, &total); err != nil || a != from {
		resp.Body.Close()
		return nil, 0, ErrNoSeek
	}
	return resp.Body, total, nil
}

func (it *item) peek(ctx context.Context, from int64, n int) ([]byte, int64, error) {
	r, total, err := it.source(ctx, from, from+int64(n)-1)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, int64(n)))
	return b, total, err
}

// Seekable reports whether Resume is likely to work, without reading the song.
func (h *Handle) Seekable() bool {
	it := h.it
	if it == nil || it.data != nil || it.head != nil || h.Duration <= 0 {
		return false
	}
	return it.ctype == "audio/mpeg" || strings.Contains(it.ctype, "wav") || strings.Contains(it.ctype, "flac")
}

// Resume hands out the same song from about at. It returns where it really starts.
func (h *Handle) Resume(ctx context.Context, at time.Duration) (*Handle, time.Duration, error) {
	it, s := h.it, h.srv
	if it == nil || it.data != nil || it.head != nil || h.Duration <= 0 || at <= 0 || at >= h.Duration {
		return nil, 0, ErrNoSeek
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	first, total, err := it.peek(ctx, 0, 64<<10)
	if err != nil {
		return nil, 0, err
	}
	var head []byte
	var off int64
	start := at
	ext := ".mp3"
	switch {
	case bytes.HasPrefix(first, []byte("fLaC")):
		ext = ".flac"
		head, off, start, err = flacAt(ctx, it, first, total, at, h.Duration)
	case bytes.HasPrefix(first, []byte("RIFF")):
		ext = ".wav"
		head, off, start, err = wavAt(ctx, it, first, total, at)
	case it.ctype == "audio/mpeg":
		off, err = mp3At(ctx, it, first, total, at, h.Duration)
		head = []byte{}
	default:
		err = ErrNoSeek
	}
	if err != nil {
		return nil, 0, err
	}
	n := &item{h: &Handle{Label: h.Label, Duration: h.Duration - start}, path: it.path, open: it.open, ctype: it.ctype, head: head, off: off, total: int64(len(head)) + total - off}
	nh, err := s.register(n, ext)
	return nh, start, err
}

// mp3At finds a frame near a time; a constant bitrate makes it exact.
func mp3At(ctx context.Context, it *item, first []byte, total int64, at, length time.Duration) (int64, error) {
	var start int64
	if len(first) > 10 && string(first[:3]) == "ID3" {
		start = 10 + int64(first[6])<<21 + int64(first[7])<<14 + int64(first[8])<<7 + int64(first[9])
		if start >= total {
			return 0, ErrNoSeek
		}
	}
	frac := float64(at) / float64(length)
	// A variable bitrate stream carries a table of contents.
	if b, _, err := it.peek(ctx, start, 4096); err == nil {
		if i := bytes.Index(b, []byte("Xing")); i >= 0 && len(b) > i+16 {
			flags := binary.BigEndian.Uint32(b[i+4:])
			p := i + 8 + 4*int(flags&1) + 4*int(flags>>1&1)
			if flags&4 != 0 && len(b) >= p+100 {
				toc := b[p : p+100]
				x := min(98, int(frac*100))
				next := 256.0
				if x < 99 {
					next = float64(toc[x+1])
				}
				frac = (float64(toc[x]) + (next-float64(toc[x]))*(frac*100-float64(x))) / 256
			}
		}
	}
	off := start + int64(float64(total-start)*frac)
	b, _, err := it.peek(ctx, off, 8192)
	if err != nil {
		return 0, err
	}
	for i := 0; i+4 < len(b); i++ {
		n := mp3Frame(b[i:])
		if n > 0 && i+n+4 < len(b) && mp3Frame(b[i+n:]) > 0 {
			return off + int64(i), nil
		}
	}
	return 0, ErrNoSeek
}

// wavAt makes a WAV of what comes after a time; its samples are all the same size.
func wavAt(ctx context.Context, it *item, first []byte, total int64, at time.Duration) ([]byte, int64, time.Duration, error) {
	if len(first) < 12 || string(first[8:12]) != "WAVE" {
		return nil, 0, 0, ErrNoSeek
	}
	var format []byte
	for pos := int64(12); pos+8 <= total; {
		var b []byte
		if pos+64 <= int64(len(first)) {
			b = first[pos : pos+64]
		} else {
			var err error
			if b, _, err = it.peek(ctx, pos, 64); err != nil {
				return nil, 0, 0, err
			}
		}
		if len(b) < 8 {
			break
		}
		n := int64(binary.LittleEndian.Uint32(b[4:]))
		switch string(b[:4]) {
		case "fmt ":
			if n < 16 || n > 40 || int64(len(b)) < 8+n {
				return nil, 0, 0, ErrNoSeek
			}
			format = bytes.Clone(b[:8+n])
		case "data":
			if format == nil {
				return nil, 0, 0, ErrNoSeek
			}
			rate, frame := int64(binary.LittleEndian.Uint32(format[16:])), int64(binary.LittleEndian.Uint16(format[20:]))
			if rate <= 0 || frame <= 0 {
				return nil, 0, 0, ErrNoSeek
			}
			skip := int64(at.Seconds()*float64(rate)) / frame * frame
			left := min(n, total-pos-8) - skip
			if n == 0 || n >= 0xFFFFFF00 { // its size was never filled in
				left = total - pos - 8 - skip
			}
			if left < frame {
				return nil, 0, 0, ErrNoSeek
			}
			// What comes after the sound, tags for one, is left over and not played.
			rest := total - pos - 8 - skip
			head := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(4+int64(len(format))+8+rest))...)
			head = append(append(head, "WAVE"...), format...)
			head = binary.LittleEndian.AppendUint32(append(head, "data"...), uint32(left))
			return head, pos + 8 + skip, time.Duration(float64(skip) / float64(rate) * float64(time.Second)), nil
		}
		pos += 8 + n + n&1
	}
	return nil, 0, 0, ErrNoSeek
}

// mp3Frame returns the length of the layer III frame starting b, or 0.
func mp3Frame(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 || b[1]>>1&3 != 1 {
		return 0
	}
	ver, br, sr := b[1]>>3&3, b[2]>>4, b[2]>>2&3
	if ver == 1 || br == 0 || br == 15 || sr == 3 {
		return 0
	}
	v1 := [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	v2 := [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	rate := [...]int{44100, 48000, 32000}[sr]
	pad := int(b[2] >> 1 & 1)
	if ver == 3 {
		return 144000*v1[br]/rate + pad
	}
	if ver == 2 {
		rate /= 2
	} else {
		rate /= 4
	}
	return 72000*v2[br]/rate + pad
}

// flacAt finds the frame playing at a time: every frame says which sample it starts with.
func flacAt(ctx context.Context, it *item, first []byte, total int64, at, length time.Duration) ([]byte, int64, time.Duration, error) {
	info, pos, err := flacStart(ctx, it, first, total)
	if err != nil {
		return nil, 0, 0, err
	}
	block := int64(binary.BigEndian.Uint16(info[0:]))
	rate := int64(info[10])<<12 | int64(info[11])<<4 | int64(info[12])>>4
	lo, hi, tlo, thi := pos, total, 0.0, length.Seconds()
	want := at.Seconds()
	var off int64
	var got float64
	for i := 0; i < 6; i++ {
		guess := lo + int64(float64(hi-lo)*(want-tlo)/(thi-tlo))
		guess = max(pos, min(total-70000, guess))
		b, _, err := it.peek(ctx, guess, 64<<10)
		if err != nil {
			return nil, 0, 0, err
		}
		j, sample := flacFrame(b, block)
		if j < 0 {
			return nil, 0, 0, ErrNoSeek
		}
		off, got = guess+int64(j), float64(sample)/float64(rate)
		if got <= want+0.2 && want-got < 1.2 {
			break
		}
		if got < want {
			lo, tlo = off, got
		} else {
			hi, thi = off, got
		}
		if thi-tlo < 0.1 || hi-lo < 1 {
			break
		}
	}
	if off <= 0 || got < want-15 || got > want+15 {
		return nil, 0, 0, ErrNoSeek
	}
	// Unknown length and checksum: what follows is not the whole song.
	info[13] &= 0xF0
	for i := 14; i < 34; i++ {
		info[i] = 0
	}
	head := append([]byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 34}, info...)
	return head, off, time.Duration(got * float64(time.Second)), nil
}

// flacStart returns a FLAC file's STREAMINFO and where its frames come, after all metadata,
// pictures included.
func flacStart(ctx context.Context, it *item, first []byte, total int64) ([]byte, int64, error) {
	if len(first) < 42 || !bytes.HasPrefix(first, []byte("fLaC")) || first[4]&0x7F != 0 {
		return nil, 0, ErrNoSeek
	}
	info := bytes.Clone(first[8:42])
	if info[10] == 0 && info[11] == 0 && info[12]>>4 == 0 { // the sample rate
		return nil, 0, ErrNoSeek
	}
	pos := int64(4)
	for {
		var b []byte
		var err error
		if pos+4 <= int64(len(first)) {
			b = first[pos : pos+4]
		} else {
			b, _, err = it.peek(ctx, pos, 4)
		}
		if err != nil {
			return nil, 0, err
		}
		if len(b) < 4 {
			return nil, 0, ErrNoSeek
		}
		pos += 4 + int64(b[1])<<16 + int64(b[2])<<8 + int64(b[3])
		if b[0]&0x80 != 0 {
			return info, pos, nil
		}
		if pos >= total {
			return nil, 0, ErrNoSeek
		}
	}
}

// flacFrame finds the first frame in b and the sample it starts with.
func flacFrame(b []byte, block int64) (int, int64) {
	for i := 0; i+20 < len(b); i++ {
		if n, ok := flacHeader(b[i:], block); ok {
			return i, n
		}
	}
	return -1, 0
}

// flacHeader reads the header of the frame starting b, at least 20 bytes: the sample it starts with.
func flacHeader(b []byte, block int64) (int64, bool) {
	if b[0] != 0xFF || b[1]&0xFE != 0xF8 || b[2]>>4 == 0 || b[2]&0x0F == 0x0F || b[3]>>4 > 10 || b[3]&1 != 0 {
		return 0, false
	}
	n, w := flacNumber(b[4:])
	if w == 0 {
		return 0, false
	}
	p := 4 + w
	switch b[2] >> 4 {
	case 6:
		p++
	case 7:
		p += 2
	}
	switch b[2] & 0x0F {
	case 12:
		p++
	case 13, 14:
		p += 2
	}
	if p >= len(b) || crc8(b[:p]) != b[p] {
		return 0, false
	}
	if b[1]&1 == 0 {
		n *= block
	}
	return n, true
}

func flacNumber(b []byte) (int64, int) {
	c := b[0]
	if c < 0x80 {
		return int64(c), 1
	}
	w := 0
	for m := byte(0x80); c&m != 0; m >>= 1 {
		w++
	}
	if w < 2 || w > 7 {
		return 0, 0
	}
	n := int64(c & (0x7F >> w))
	for k := 1; k < w; k++ {
		if b[k]&0xC0 != 0x80 {
			return 0, 0
		}
		n = n<<6 | int64(b[k]&0x3F)
	}
	return n, w
}

func crc8(b []byte) byte {
	var c byte
	for _, x := range b {
		c ^= x
		for i := 0; i < 8; i++ {
			if c&0x80 != 0 {
				c = c<<1 ^ 7
			} else {
				c <<= 1
			}
		}
	}
	return c
}

// middle serves a song from its middle as if it were a whole file.
func (s *Server) middle(w http.ResponseWriter, r *http.Request, it *item) {
	from, to := int64(0), it.total-1
	code := http.StatusOK
	if rng := strings.ReplaceAll(r.Header.Get("Range"), " ", ""); strings.HasPrefix(rng, "bytes=") {
		a, b, _ := strings.Cut(rng[6:], "-")
		if a == "" {
			if n, err := strconv.ParseInt(b, 10, 64); err == nil {
				from = max(0, it.total-n)
			}
		} else if n, err := strconv.ParseInt(a, 10, 64); err == nil {
			from = n
			if m, err := strconv.ParseInt(b, 10, 64); err == nil {
				to = min(to, m)
			}
		}
		if from > to {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", it.total))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		code = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, it.total))
	}
	h := int64(len(it.head))
	var body io.Reader = bytes.NewReader(nil)
	if to >= h {
		sctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		src, srcTotal, err := it.source(sctx, it.off+max(0, from-h), it.off+to-h)
		if err != nil {
			http.Error(w, "source unavailable", http.StatusBadGateway)
			return
		}
		defer src.Close()
		body = src
		if it.open != nil {
			st := &steady{ctx: r.Context(), total: srcTotal, open: it.open, body: src, cancel: cancel, off: it.off + max(0, from-h), end: it.off + to - h + 1, note: s.reopened(it)}
			defer st.Close()
			body = st
		}
	}
	if from < h {
		body = io.MultiReader(bytes.NewReader(it.head[from:]), body)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(to-from+1, 10))
	w.WriteHeader(code)
	if r.Method != http.MethodHead {
		n, err := io.CopyN(w, body, to-from+1)
		if s.Log != nil && !it.here {
			s.Log.Info("音频　续播传完", "音频", it.h.Label, "字节", n, "完整", err == nil)
		}
	}
}
