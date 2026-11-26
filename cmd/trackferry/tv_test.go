package main

import (
	"errors"
	"testing"
)

func TestServerName(t *testing.T) {
	for in, want := range map[string]string{
		"Coco":         "Coco",
		" fnos.local ": "fnos",
		"":             "TrackFerry",
		"localhost":    "TrackFerry",
		"3f2a9c01be47": "TrackFerry",
		"3f2a9c01be473f2a9c01be473f2a9c01be473f2a9c01be473f2a9c01be47aaaa": "TrackFerry",
		"书房的飞牛": "书房的飞牛",
	} {
		if got := serverName(func() (string, error) { return in, nil }); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := serverName(func() (string, error) { return "Coco", errors.New("no") }); got != "TrackFerry" {
		t.Errorf("error: got %q", got)
	}
}
