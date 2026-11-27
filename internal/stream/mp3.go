package stream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Lossless is whether a file of this extension is converted for speakers that ask for MP3.
// Not .m4a: it is AAC as often as ALAC, and AAC to MP3 only loses.
func Lossless(ext string) bool {
	switch strings.ToLower(ext) {
	case ".flac", ".wav", ".ape", ".aiff", ".aif", ".wv", ".dsf", ".dff":
		return true
	}
	return false
}

// Lossless reports whether the handle serves a lossless original.
func (h *Handle) Lossless() bool {
	it := h.it
	return it != nil && it.ctype != "audio/mpeg" && (strings.Contains(it.ctype, "flac") || strings.Contains(it.ctype, "wav") || Lossless(filepath.Ext(it.path)))
}

// ErrNotLossless means the song is played as it is: there is nothing to convert.
var ErrNotLossless = errors.New("not a lossless file")

// FindFFmpeg returns the ffmpeg to use: the configured one, else the one on PATH, else "".
func FindFFmpeg(configured string) string {
	if configured != "" {
		if p, err := exec.LookPath(configured); err == nil {
			return p
		}
		return ""
	}
	p, _ := exec.LookPath("ffmpeg")
	return p
}

// MP3 converts lossless songs to 320 kbps MP3 with ffmpeg, whole, and keeps the results in
// a directory of bounded size. A conversion that fails leaves nothing behind.
type MP3 struct {
	FFmpeg string // path of the ffmpeg binary
	Dir    string
	Cap    int64         // bytes the directory may hold; the least recently used go first
	Limit  time.Duration // longest a conversion may take

	mu      sync.Mutex
	running map[string]*conversion
	slot    chan struct{} // one ffmpeg at a time: a small NAS is not to be swamped
}

type conversion struct {
	done chan struct{}
	path string
	err  error
}

// NewMP3 prepares the converter; dir is created when needed.
func NewMP3(ffmpeg, dir string, capBytes int64) *MP3 {
	return &MP3{FFmpeg: ffmpeg, Dir: dir, Cap: capBytes, Limit: 3 * time.Minute, running: map[string]*conversion{}, slot: make(chan struct{}, 1)}
}

// Cached returns the converted file of key when there is one.
func (m *MP3) Cached(key string) (string, bool) {
	p := m.pathOf(key)
	if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
		now := time.Now()
		os.Chtimes(p, now, now) // recently used: kept longest
		return p, true
	}
	return "", false
}

func (m *MP3) pathOf(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(m.Dir, hex.EncodeToString(sum[:16])+".mp3")
}

// Convert returns the MP3 of key, converting it first. in is a file path, or "-" with body
// carrying the source (it is closed). Callers asking for the same key share one conversion.
func (m *MP3) Convert(ctx context.Context, key, in string, body io.ReadCloser) (string, error) {
	if p, ok := m.Cached(key); ok {
		if body != nil {
			body.Close()
		}
		return p, nil
	}
	m.mu.Lock()
	c, ok := m.running[key]
	if !ok {
		c = &conversion{done: make(chan struct{})}
		m.running[key] = c
		go func() {
			c.path, c.err = m.convert(key, in, body)
			m.mu.Lock()
			delete(m.running, key)
			m.mu.Unlock()
			close(c.done)
		}()
	} else if body != nil {
		body.Close()
	}
	m.mu.Unlock()
	select {
	case <-c.done:
		return c.path, c.err
	case <-ctx.Done():
		return "", ctx.Err() // the conversion goes on and is kept for the next time
	}
}

func (m *MP3) convert(key, in string, body io.ReadCloser) (string, error) {
	if body != nil {
		defer body.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.Limit)
	defer cancel()
	select {
	case m.slot <- struct{}{}:
		defer func() { <-m.slot }()
	case <-ctx.Done():
		return "", errors.New("转码排队超时")
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return "", err
	}
	out := m.pathOf(key)
	tmp := out + ".part"
	os.Remove(tmp)
	// 44.1 kHz stereo at a constant 320 kbps: every speaker plays it and a start in the middle
	// is exact. No tags and no cover: some firmware chokes on a large ID3 block.
	cmd := exec.CommandContext(ctx, m.FFmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", in, "-map", "0:a:0", "-vn", "-map_metadata", "-1", "-ac", "2", "-ar", "44100",
		"-c:a", "libmp3lame", "-b:a", "320k", "-id3v2_version", "0", "-write_id3v1", "0", "-f", "mp3", tmp)
	if body != nil {
		cmd.Stdin = body
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 200 {
			msg = msg[len(msg)-200:]
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("转码超过 %d 秒", int(m.Limit.Seconds()))
		}
		return "", fmt.Errorf("ffmpeg 失败：%v %s", err, msg)
	}
	if fi, err := os.Stat(tmp); err != nil || fi.Size() == 0 {
		os.Remove(tmp)
		return "", errors.New("ffmpeg 没有输出")
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", err
	}
	m.trim(out)
	return out, nil
}

// trim deletes the least recently used files until the directory fits its cap, never keep.
func (m *MP3) trim(keep string) {
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mp3") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{filepath.Join(m.Dir, e.Name()), fi.Size(), fi.ModTime()})
		total += fi.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if total <= m.Cap {
			break
		}
		if f.path == keep {
			continue
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// SongMP3 registers a lossless library song as MP3, converting it first (or taking the
// converted file from before). ErrNotLossless: the song needs no converting.
func (s *Server) SongMP3(ctx context.Context, rel, label string, d time.Duration) (*Handle, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	out, err := s.mp3Of(ctx, rel)
	if err != nil {
		return nil, err
	}
	if label == "" {
		label = filepath.Base(rel)
	}
	return s.register(&item{h: &Handle{Label: label, Duration: d}, path: out, ctype: "audio/mpeg", name: strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)) + ".mp3"}, ".mp3")
}

// WarmMP3 converts a song ahead of its turn, so that it starts at once; errors are dropped.
func (s *Server) WarmMP3(rel string) {
	if s.MP3 == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.MP3.Limit)
		defer cancel()
		if _, err := s.mp3Of(ctx, rel); err != nil && !errors.Is(err, ErrNotLossless) && s.Log != nil {
			s.Log.Warn("转码　提前转下一首失败", "原因", err)
		}
	}()
}

func (s *Server) mp3Of(ctx context.Context, rel string) (string, error) {
	if s.MP3 == nil {
		return "", errors.New("没有 ffmpeg")
	}
	if s.Remote != nil && strings.HasPrefix(rel, s.RemotePrefix) {
		open, ext := s.Remote(rel)
		if open == nil {
			return "", errors.New("song is no longer in the library")
		}
		if !Lossless(ext) {
			return "", ErrNotLossless
		}
		if p, ok := s.MP3.Cached(rel); ok {
			return p, nil
		}
		// Opened apart from the caller: a conversion it gave up on still finishes and is kept.
		resp, err := open(context.WithoutCancel(ctx), "")
		if err != nil {
			return "", err
		}
		if resp.StatusCode/100 != 2 {
			resp.Body.Close()
			return "", fmt.Errorf("source answered %d", resp.StatusCode)
		}
		return s.MP3.Convert(ctx, rel, "-", resp.Body)
	}
	it, ext, err := s.file(rel, "")
	if err != nil {
		return "", err
	}
	if !Lossless(ext) {
		return "", ErrNotLossless
	}
	fi, err := os.Stat(it.path)
	if err != nil {
		return "", err
	}
	return s.MP3.Convert(ctx, fmt.Sprintf("%s|%d|%d", it.path, fi.Size(), fi.ModTime().UnixNano()), it.path, nil)
}
