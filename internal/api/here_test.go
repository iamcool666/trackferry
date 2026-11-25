package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"trackferry/internal/stream"
)

func TestHereTakesAnOnlineSongOnce(t *testing.T) {
	var h here
	n := 0
	find := func() (stream.Opener, string, error) { n++; return nil, "歌", nil }
	a, b := h.take("kuwo:1", find), h.take("kuwo:1", find)
	if n != 1 || a != b || a.label != "歌" {
		t.Fatal(n)
	}
	h.drop("kuwo:1", a)
	if h.take("kuwo:1", find); n != 2 {
		t.Fatal(n)
	}
	bad := errors.New("x")
	for range 2 { // what failed is asked for again
		if x := h.take("kuwo:2", func() (stream.Opener, string, error) { n++; return nil, "", bad }); x.err != bad {
			t.Fatal(x.err)
		}
	}
	if n != 4 {
		t.Fatal(n)
	}
}

func TestSeekAsksForAPosition(t *testing.T) {
	s, tok, _ := homeServer(t)
	for body, want := range map[string]string{`{"action":"seek"}`: "bad_position", `{"action":"seek","position":-1}`: "bad_position"} {
		if rec := call(s.Handler(), "POST", "/api/devices/d1/control", tok, body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Fatal(body, rec.Code, rec.Body)
		}
	}
}
