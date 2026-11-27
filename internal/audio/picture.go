package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
)

// ErrNoPicture means the file carries no cover of its own.
var ErrNoPicture = errors.New("no embedded picture")

// maxPicture bounds an embedded cover; anything larger is not served.
const maxPicture = 8 << 20

// Picture returns the cover embedded in an audio file: the front cover when it is marked,
// otherwise the first picture. ext is lower case with the dot.
func Picture(p, ext string) (mime string, data []byte, err error) {
	f, err := os.Open(p)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	switch ext {
	case ".flac":
		data, err = flacPicture(f)
	case ".mp3":
		data, err = mp3Picture(f)
	case ".m4a":
		data, err = mp4Picture(f, fi.Size())
	default:
		err = ErrNoPicture
	}
	if err != nil {
		return "", nil, err
	}
	mime = http.DetectContentType(data)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return mime, data, nil
	}
	return "", nil, ErrNoPicture
}

func flacPicture(f io.ReadSeeker) ([]byte, error) {
	var head [10]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return nil, ErrNoPicture
	}
	start := int64(0)
	if string(head[:3]) == "ID3" {
		start = 10 + int64(syncsafe(head[6:10]))
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil || string(magic[:]) != "fLaC" {
		return nil, ErrNoPicture
	}
	var first []byte
	for range 64 {
		var h [4]byte
		if _, err := io.ReadFull(f, h[:]); err != nil {
			break
		}
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		if h[0]&0x7f == 6 && n <= maxPicture+4096 {
			b := make([]byte, n)
			if _, err := io.ReadFull(f, b); err != nil {
				break
			}
			if kind, data, ok := flacBlock(b); ok {
				if kind == 3 {
					return data, nil
				}
				if first == nil {
					first = data
				}
			}
		} else if _, err := f.Seek(n, io.SeekCurrent); err != nil {
			break
		}
		if h[0]&0x80 != 0 {
			break
		}
	}
	if first == nil {
		return nil, ErrNoPicture
	}
	return first, nil
}

// flacBlock reads a METADATA_BLOCK_PICTURE (also what Vorbis comments carry).
func flacBlock(b []byte) (kind uint32, data []byte, ok bool) {
	be := binary.BigEndian
	take := func(n int) []byte {
		if n < 0 || n > len(b) {
			return nil
		}
		v := b[:n]
		b = b[n:]
		return v
	}
	u := func() (uint32, bool) {
		v := take(4)
		if v == nil {
			return 0, false
		}
		return be.Uint32(v), true
	}
	kind, ok1 := u()
	ml, ok2 := u()
	if !ok1 || !ok2 || take(int(ml)) == nil {
		return 0, nil, false
	}
	dl, ok3 := u()
	if !ok3 || take(int(dl)) == nil || take(16) == nil {
		return 0, nil, false
	}
	n, ok4 := u()
	if !ok4 || n == 0 || int(n) > len(b) {
		return 0, nil, false
	}
	return kind, b[:n], true
}

func mp3Picture(f io.ReadSeeker) ([]byte, error) {
	var head [10]byte
	if _, err := io.ReadFull(f, head[:]); err != nil || string(head[:3]) != "ID3" {
		return nil, ErrNoPicture
	}
	n := syncsafe(head[6:10])
	b := make([]byte, min(n, maxTag))
	k, _ := io.ReadFull(f, b)
	ver := head[3]
	var first []byte
	id3frames(ver, head[5], b[:k], func(id string, body []byte) bool {
		if id != "APIC" && id != "PIC" || len(body) < 4 {
			return true
		}
		enc := body[0]
		rest := body[1:]
		if id == "PIC" {
			rest = rest[3:] // three-letter image format
		} else {
			i := bytes.IndexByte(rest, 0)
			if i < 0 {
				return true
			}
			rest = rest[i+1:]
		}
		if len(rest) < 1 {
			return true
		}
		kind := rest[0]
		rest = rest[1:]
		// The description ends with one zero byte, or two for UTF-16.
		if enc == 1 || enc == 2 {
			i := 0
			for ; i+1 < len(rest); i += 2 {
				if rest[i] == 0 && rest[i+1] == 0 {
					break
				}
			}
			if i+2 > len(rest) {
				return true
			}
			rest = rest[i+2:]
		} else {
			i := bytes.IndexByte(rest, 0)
			if i < 0 {
				return true
			}
			rest = rest[i+1:]
		}
		if len(rest) == 0 {
			return true
		}
		if kind == 3 {
			first = rest
			return false
		}
		if first == nil {
			first = rest
		}
		return true
	})
	if first == nil {
		return nil, ErrNoPicture
	}
	return first, nil
}

func mp4Picture(f io.ReadSeeker, size int64) ([]byte, error) {
	var out []byte
	var walk func(off, n int64, depth int) error
	walk = func(off, n int64, depth int) error {
		if depth > 6 {
			return nil
		}
		return mp4Boxes(f, off, n, func(kind string, off, n int64) error {
			switch kind {
			case "moov", "udta", "ilst":
				return walk(off, n, depth+1)
			case "meta":
				if n < 4 {
					return nil
				}
				return walk(off+4, n-4, depth+1)
			case "covr":
				return mp4Boxes(f, off, n, func(kind string, off, n int64) error {
					if kind != "data" || out != nil || n <= 8 {
						return nil
					}
					b, err := readAt(f, off, n, maxPicture+8)
					if err != nil {
						return nil
					}
					out = b[8:]
					return nil
				})
			}
			return nil
		})
	}
	if err := walk(0, size, 0); err != nil && out == nil {
		return nil, ErrNoPicture
	}
	if out == nil {
		return nil, ErrNoPicture
	}
	return out, nil
}

// Lyrics returns lyrics stored in an audio file's tags (ID3 USLT, Vorbis LYRICS, MP4 ©lyr),
// or "" when there are none.
func Lyrics(p, ext string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	switch ext {
	case ".mp3":
		var head [10]byte
		if _, err := io.ReadFull(f, head[:]); err != nil || string(head[:3]) != "ID3" {
			return "", nil
		}
		b := make([]byte, min(syncsafe(head[6:10]), maxTag))
		k, _ := io.ReadFull(f, b)
		out := ""
		id3frames(head[3], head[5], b[:k], func(id string, body []byte) bool {
			if id != "USLT" && id != "ULT" || len(body) < 5 {
				return true
			}
			enc, rest := body[0], body[4:] // encoding, language
			if enc == 1 || enc == 2 {
				i := 0
				for ; i+1 < len(rest) && !(rest[i] == 0 && rest[i+1] == 0); i += 2 {
				}
				if i+2 > len(rest) {
					return true
				}
				rest = rest[i+2:]
			} else if i := bytes.IndexByte(rest, 0); i >= 0 {
				rest = rest[i+1:]
			}
			out = id3text(append([]byte{enc}, rest...))
			return out == ""
		})
		return out, nil
	case ".flac":
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		var magic [4]byte
		if _, err := io.ReadFull(f, magic[:]); err != nil || string(magic[:]) != "fLaC" {
			return "", nil
		}
		for range 64 {
			var h [4]byte
			if _, err := io.ReadFull(f, h[:]); err != nil {
				break
			}
			n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
			if h[0]&0x7f == 4 && n <= maxTag {
				b := make([]byte, n)
				if _, err := io.ReadFull(f, b); err != nil {
					break
				}
				return vorbisField(b, "LYRICS", "UNSYNCEDLYRICS"), nil
			} else if _, err := f.Seek(n, io.SeekCurrent); err != nil {
				break
			}
			if h[0]&0x80 != 0 {
				break
			}
		}
		return "", nil
	case ".m4a":
		out := ""
		var walk func(off, n int64, depth int) error
		walk = func(off, n int64, depth int) error {
			if depth > 6 {
				return nil
			}
			return mp4Boxes(f, off, n, func(kind string, off, n int64) error {
				switch kind {
				case "moov", "udta", "ilst":
					return walk(off, n, depth+1)
				case "meta":
					if n >= 4 {
						return walk(off+4, n-4, depth+1)
					}
				case "\xa9lyr":
					if b, err := readAt(f, off, n, 512<<10); err == nil && len(b) > 16 && string(b[4:8]) == "data" {
						out = string(b[16:])
					}
				}
				return nil
			})
		}
		walk(0, fi.Size(), 0)
		return out, nil
	}
	return "", nil
}

// vorbisField is the first of the named comments in a Vorbis comment block.
func vorbisField(b []byte, keys ...string) string {
	le := binary.LittleEndian
	next := func() ([]byte, bool) {
		if len(b) < 4 {
			return nil, false
		}
		n := uint64(le.Uint32(b))
		if n > uint64(len(b)-4) {
			return nil, false
		}
		v := b[4 : 4+n]
		b = b[4+n:]
		return v, true
	}
	if _, ok := next(); !ok || len(b) < 4 {
		return ""
	}
	count := le.Uint32(b)
	b = b[4:]
	for range min(count, 1000) {
		c, ok := next()
		if !ok {
			break
		}
		k, v, _ := bytes.Cut(c, []byte("="))
		for _, want := range keys {
			if bytes.EqualFold(k, []byte(want)) && len(v) > 0 {
				return string(v)
			}
		}
	}
	return ""
}
