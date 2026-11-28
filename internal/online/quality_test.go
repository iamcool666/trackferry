package online

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type qualitySource struct {
	base  string
	asked []Quality
}

func (*qualitySource) Name() string                                        { return "q" }
func (*qualitySource) Search(context.Context, string) ([]Candidate, error) { return nil, nil }
func (q *qualitySource) Resolve(ctx context.Context, _ Candidate) (string, error) {
	q.asked = append(q.asked, QualityOf(ctx))
	if QualityOf(ctx) != MP3128 {
		return "", ErrRestricted // 320k for members only
	}
	return q.base + "/a.mp3", nil
}

func TestSelectedQualityFallsThroughEveryLowerTier(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0})
	mp3 := bytes.Repeat(frame, 7000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp3", time.Time{}, bytes.NewReader(mp3))
	}))
	defer srv.Close()
	src := &qualitySource{base: srv.URL}
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), srv.Client(), src)
	if s.Quality() != HiRes || s.SetQuality("lossless") {
		t.Fatal("high is the default and only the two qualities exist")
	}
	st, err := s.resolveOne(context.Background(), src, Candidate{ID: "1", Duration: 180 * time.Second})
	if err != nil || st.URL == "" || len(src.asked) != 4 || src.asked[0] != HiRes || src.asked[1] != MP3320 || src.asked[2] != MP3192 || src.asked[3] != MP3128 {
		t.Fatalf("%v %v %v", st, err, src.asked)
	}
	src.asked = nil
	s.SetQuality(MP3192)
	if _, err := s.resolveOne(context.Background(), src, Candidate{ID: "1", Duration: 180 * time.Second}); err != nil || len(src.asked) != 2 || src.asked[0] != MP3192 || src.asked[1] != MP3128 {
		t.Fatalf("selected 192 falls only to 128: %v %v", err, src.asked)
	}
}
