package library

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func TestSidecarLyrics(t *testing.T) {
	root := t.TempDir()
	library := New(root, nil, nil)
	lyric := "[00:01.20]第一句"
	legacy, err := simplifiedchinese.GBK.NewEncoder().String(lyric)
	if err != nil {
		t.Fatal(err)
	}
	wide, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().String(lyric)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"utf8.lrc": "\ufeff" + lyric, "legacy.LRC": legacy, "wide.lrc": wide} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		audio := name[:len(name)-4] + ".flac"
		if got, err := library.SidecarLyrics(audio); err != nil || got != lyric {
			t.Fatalf("%s: %q, %v", audio, got, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "other.lrc")
	if err := os.WriteFile(outside, []byte(lyric), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "other.lrc")); err != nil {
		t.Fatal(err)
	}
	if got, _ := library.SidecarLyrics("other.flac"); got != "" {
		t.Fatal("read lyrics outside the music root")
	}
}
