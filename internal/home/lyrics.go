package home

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Line is one line of lyrics. T is when it is sung; null when the lyrics carry no times.
type Line struct {
	T    *int64 `json:"t"`
	Text string `json:"text"`
}

var (
	lrcTag    = regexp.MustCompile(`(?i)\[(?:\d+:[\d.:]+|[a-z]+:[^\]]*)\]|<\d+:[\d.:]+>`)
	lrcTime   = regexp.MustCompile(`\[(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?\]`)
	lrcOffset = regexp.MustCompile(`(?i)^\[offset:\s*([+-]?\d{1,7})\s*\]`)
)

// Lyrics reads LRC, with its offset already applied, or plain words.
func Lyrics(text string) (synced bool, lines []Line) {
	lines = []Line{}
	plain := []Line{}
	var shift int64
	rows := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	for _, raw := range rows {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if m := lrcOffset.FindStringSubmatch(raw); m != nil {
			shift, _ = strconv.ParseInt(m[1], 10, 64)
		}
	}
	for _, raw := range rows {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if lrcOffset.MatchString(raw) {
			continue
		}
		words := strings.TrimSpace(lrcTag.ReplaceAllString(raw, ""))
		if words == "" {
			continue
		}
		plain = append(plain, Line{Text: words})
		for _, m := range lrcTime.FindAllStringSubmatch(raw, -1) {
			minute, err := strconv.ParseInt(m[1], 10, 32)
			if err != nil {
				continue
			}
			sec, _ := strconv.ParseInt(m[2], 10, 64)
			ms, _ := strconv.ParseInt((m[3] + "000")[:3], 10, 64)
			t := minute*60000 + sec*1000 + ms - shift
			lines = append(lines, Line{&t, words})
		}
	}
	if len(lines) == 0 {
		return false, plain
	}
	slices.SortStableFunc(lines, func(a, b Line) int { return cmp.Compare(*a.T, *b.T) })
	merged := lines[:0]
	for _, line := range lines {
		if len(merged) == 0 || *merged[len(merged)-1].T != *line.T {
			merged = append(merged, line)
		} else if previous := &merged[len(merged)-1]; !slices.Contains(strings.Split(previous.Text, "\n"), line.Text) {
			previous.Text += "\n" + line.Text
		}
	}
	return true, merged
}
