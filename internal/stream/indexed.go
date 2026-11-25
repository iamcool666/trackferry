package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// FLAC has no index a WebKit browser would use: after a jump it guesses by bytes, and says it
// is where it was told to go while it plays something else. The same frames inside an MP4 that
// lists them are found to the sample.

// indexes keeps what songs looked like; a browser asks for one in many pieces.
type indexes struct {
	mu sync.Mutex
	m  map[string]*indexed
}

type indexed struct {
	once sync.Once
	at   time.Time
	head []byte // nil if the song cannot be indexed
	off  int64
	size int64
}

const keptIndexes = 32

func (x *indexes) get(key string) *indexed {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.m == nil {
		x.m = map[string]*indexed{}
	}
	i := x.m[key]
	if i == nil {
		i = &indexed{}
		x.m[key] = i
	}
	i.at = time.Now()
	for len(x.m) > keptIndexes {
		old := ""
		for k, v := range x.m {
			if old == "" || v.at.Before(x.m[old].at) {
				old = k
			}
		}
		delete(x.m, old)
	}
	return i
}

func (x *indexes) drop(key string, i *indexed) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.m[key] == i {
		delete(x.m, key)
	}
}

// index makes a FLAC song an MP4. Other songs, and those it cannot read, stay as they are.
func (s *Server) index(ctx context.Context, key string, it *item) {
	for range 2 {
		x := s.indexes.get(key)
		x.once.Do(func() {
			// Others may be waiting for it, so it does not end with this request.
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
			defer cancel()
			var err error
			if x.head, x.off, x.size, err = flacIndex(c, it); err != nil && !errors.Is(err, ErrNoSeek) {
				s.indexes.drop(key, x) // it may be readable another time
			}
		})
		if x.head == nil {
			return
		}
		if _, size, err := it.peek(ctx, 0, 4); err == nil && size != x.size {
			s.indexes.drop(key, x) // the file has changed
			continue
		}
		it.head, it.off, it.total, it.ctype = x.head, x.off, int64(len(x.head))+x.size-x.off, "audio/mp4"
		return
	}
}

// flacIndex returns what stands before the frames of a FLAC file in an MP4, and where its
// frames and the file end.
func flacIndex(ctx context.Context, it *item) (head []byte, off, size int64, err error) {
	first, size, err := it.peek(ctx, 0, 64<<10)
	if err != nil {
		return nil, 0, 0, err
	}
	info, pos, err := flacStart(ctx, it, first, size)
	if err != nil {
		return nil, 0, 0, err
	}
	r, _, err := it.source(ctx, pos, -1)
	if err != nil {
		return nil, 0, 0, err
	}
	defer r.Close()
	block := int64(binary.BigEndian.Uint16(info))
	var starts []int64 // of the frames, from pos
	var times []int64  // and the samples they start with
	buf := make([]byte, 0, 1<<20)
	base := int64(0) // where buf starts
	const tail = 32  // a header may go on in the next piece
	for end := false; !end; {
		n, rerr := io.ReadFull(r, buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if rerr != nil {
			if end = rerr == io.EOF || rerr == io.ErrUnexpectedEOF; !end {
				return nil, 0, 0, rerr
			}
		}
		last := len(buf) - tail
		i := 0
		for i < last {
			j := bytes.IndexByte(buf[i:last], 0xFF)
			if j < 0 {
				i = last
				break
			}
			i += j
			if num, ok := flacHeader(buf[i:], block); ok {
				// The numbers go up by one block; what else looks like a header is sound.
				ok = len(times) == 0 && num == 0
				if len(times) > 0 {
					step := num - times[len(times)-1]
					ok = step > 0 && step <= 65535 && (buf[i+1]&1 == 1 || step == block)
				}
				if ok {
					starts, times = append(starts, base+int64(i)), append(times, num)
				}
			}
			i++
		}
		if i = max(i, 0); i > 0 {
			base += int64(i)
			buf = buf[:copy(buf, buf[i:])]
		}
	}
	if base+int64(len(buf)) != size-pos {
		return nil, 0, 0, io.ErrUnexpectedEOF
	}
	if len(starts) < 2 || starts[0] != 0 {
		return nil, 0, 0, ErrNoSeek
	}
	total := int64(info[13]&15)<<32 | int64(binary.BigEndian.Uint32(info[14:]))
	lastLen := total - times[len(times)-1]
	if total == 0 {
		lastLen = times[1]
	} else if lastLen <= 0 || lastLen > int64(binary.BigEndian.Uint16(info[2:])) { // some frame was not found
		return nil, 0, 0, ErrNoSeek
	}
	return mp4Head(info, starts, times, lastLen, size-pos), pos, size, nil
}

func mp4Box(name string, parts ...[]byte) []byte {
	b := append(make([]byte, 4, 64), name...)
	for _, p := range parts {
		b = append(b, p...)
	}
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	return b
}

func be(v ...uint32) []byte {
	var b []byte
	for _, x := range v {
		b = binary.BigEndian.AppendUint32(b, x)
	}
	return b
}

// mp4Head is all of an MP4 up to the frames, which are its only chunk.
func mp4Head(info []byte, starts, times []int64, lastLen, size int64) []byte {
	rate := uint32(info[10])<<12 | uint32(info[11])<<4 | uint32(info[12])>>4
	channels := uint32(info[12]>>1&7) + 1
	bits := uint32(info[12]&1)<<4 | uint32(info[13]>>4) + 1
	n := len(starts)
	total := uint32(times[n-1] + lastLen)
	var sizes, runs []byte
	entries, count, same := uint32(0), uint32(0), int64(-1)
	for i := range starts {
		end, d := size, lastLen
		if i+1 < n {
			end, d = starts[i+1], times[i+1]-times[i]
		}
		sizes = binary.BigEndian.AppendUint32(sizes, uint32(end-starts[i]))
		if d != same && count > 0 {
			runs, entries, count = append(runs, be(count, uint32(same))...), entries+1, 0
		}
		same = d
		count++
	}
	runs, entries = append(runs, be(count, uint32(same))...), entries+1
	fixed := rate << 16
	if rate > 65535 {
		fixed = 0 // dfLa has it
	}
	matrix := be(0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000)
	entry := mp4Box("fLaC", be(0, 1), be(0, 0), be(channels<<16|bits), be(0), be(fixed), mp4Box("dfLa", be(0), []byte{0x80, 0, 0, 34}, info))
	ftyp := mp4Box("ftyp", []byte("isom"), be(512), []byte("isomiso2mp41"))
	moov := func(at uint32) []byte {
		stbl := mp4Box("stbl", mp4Box("stsd", be(0, 1), entry), mp4Box("stts", be(0, entries), runs), mp4Box("stsc", be(0, 1, 1, uint32(n), 1)),
			mp4Box("stsz", be(0, 0, uint32(n)), sizes), mp4Box("stco", be(0, 1, at)))
		minf := mp4Box("minf", mp4Box("smhd", be(0, 0)), mp4Box("dinf", mp4Box("dref", be(0, 1), mp4Box("url ", be(1)))), stbl)
		mdia := mp4Box("mdia", mp4Box("mdhd", be(0, 0, 0, rate, total, 0x55C40000)), mp4Box("hdlr", be(0, 0), []byte("soun"), make([]byte, 13)), minf)
		trak := mp4Box("trak", mp4Box("tkhd", be(7, 0, 0, 1, 0, total, 0, 0, 0, 0x01000000), matrix, be(0, 0)), mdia)
		return mp4Box("moov", mp4Box("mvhd", be(0, 0, 0, rate, total, 0x10000, 0x01000000, 0, 0), matrix, make([]byte, 24), be(2)), trak)
	}
	at := uint32(len(ftyp) + len(moov(0)) + 8)
	head := append(ftyp, moov(at)...)
	return append(head, append(be(uint32(size+8)), "mdat"...)...)
}

// SongHere answers a browser's request for a library song. Nothing is registered, so it works
// without a public base URL. With index, FLAC comes as an MP4.
func (s *Server) SongHere(w http.ResponseWriter, r *http.Request, rel, label string, index bool) error {
	it, ext, err := s.song(rel, label, 0)
	if err != nil {
		return err
	}
	if index && ext == ".flac" {
		s.index(r.Context(), rel, it)
	}
	s.heard(w, r, it)
	return nil
}
