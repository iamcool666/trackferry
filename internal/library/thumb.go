package library

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	_ "image/gif" // decoders for covers
	_ "image/png"
)

// Embedded covers are often several megabytes; a page of artists asks for hundreds. Covers are
// made small once, kept on disk, and at most two are decoded at a time so a large library
// cannot tie up the NAS.
const (
	thumbKeep = 512 << 20 // bytes of small covers kept
	thumbTrim = 384 << 20 // what a trim leaves
)

var (
	decoding = make(chan struct{}, 2)
	thumbMu  sync.Mutex // one trim at a time
)

// SetThumbs sets where small covers are kept; empty keeps none.
func (l *Library) SetThumbs(dir string) { l.thumbs = dir }

// CoverTag names a cover's current contents, for the browser's If-None-Match.
func (l *Library) CoverTag(id string, size int) (string, error) {
	src, err := l.coverSource(id)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return "", ErrNoCover
	}
	h := sha1.Sum(fmt.Appendf(nil, "%s\x00%d\x00%d\x00%d", id, size, fi.Size(), fi.ModTime().UnixNano()))
	return hex.EncodeToString(h[:12]), nil
}

// SizedCover is Cover no wider than size pixels (0: as it is).
func (l *Library) SizedCover(id string, size int) (mime string, data []byte, err error) {
	tag, err := l.CoverTag(id, size)
	if err != nil {
		return "", nil, err
	}
	keep := ""
	if l.thumbs != "" && size > 0 {
		keep = filepath.Join(l.thumbs, tag[:2], tag+".jpg")
		if b, err := os.ReadFile(keep); err == nil {
			return "image/jpeg", b, nil
		}
	}
	decoding <- struct{}{}
	defer func() { <-decoding }()
	mime, data, err = l.Cover(id)
	if err != nil || size <= 0 {
		return mime, data, err
	}
	small, ok := shrink(data, size)
	if !ok {
		return mime, data, nil // already small, or a format kept as it is
	}
	if keep != "" {
		if os.MkdirAll(filepath.Dir(keep), 0o755) == nil {
			tmp := keep + ".part"
			if os.WriteFile(tmp, small, 0o644) == nil && os.Rename(tmp, keep) == nil {
				if l.thumbWrites.Add(1)%200 == 0 {
					go trimThumbs(l.thumbs)
				}
			} else {
				os.Remove(tmp)
			}
		}
	}
	return "image/jpeg", small, nil
}

// coverSource is the file a cover ID reads from.
func (l *Library) coverSource(id string) (string, error) {
	if !strings.HasPrefix(id, LocalCover) || !l.index().covers[id] {
		return "", ErrNoCover
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, LocalCover))
	if err != nil {
		return "", ErrNoCover
	}
	rel := string(raw)
	if !safeRel(rel) {
		return "", ErrNoCover
	}
	return filepath.Join(l.dir, filepath.FromSlash(rel)), nil
}

// shrink re-encodes a picture at most size pixels on its long side.
func shrink(b []byte, size int) ([]byte, bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || max(cfg.Width, cfg.Height) <= size || cfg.Width*cfg.Height > 40_000_000 {
		return nil, false
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, false
	}
	r := src.Bounds()
	w, h := size, size
	if r.Dx() > r.Dy() {
		h = max(1, r.Dy()*size/r.Dx())
	} else {
		w = max(1, r.Dx()*size/r.Dy())
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// Average a few samples per pixel: enough for a cover, cheap without a resampling library.
	const n = 3
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rs, gs, bs uint32
			for j := 0; j < n; j++ {
				sy := r.Min.Y + ((y*n+j)*2+1)*r.Dy()/(h*n*2)
				for i := 0; i < n; i++ {
					sx := r.Min.X + ((x*n+i)*2+1)*r.Dx()/(w*n*2)
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					rs, gs, bs = rs+cr, gs+cg, bs+cb
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(rs / (n * n) >> 8), uint8(gs / (n * n) >> 8), uint8(bs / (n * n) >> 8), 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 84}); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

// trimThumbs drops the oldest small covers once they take more than thumbKeep.
func trimThumbs(dir string) {
	if !thumbMu.TryLock() {
		return
	}
	defer thumbMu.Unlock()
	type kept struct {
		p    string
		size int64
		at   int64
	}
	var all []kept
	var total int64
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			all = append(all, kept{p, fi.Size(), fi.ModTime().UnixNano()})
			total += fi.Size()
		}
		return nil
	})
	if total <= thumbKeep {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at < all[j].at })
	for _, k := range all {
		if total <= thumbTrim {
			break
		}
		if os.Remove(k.p) == nil {
			total -= k.size
		}
	}
}
