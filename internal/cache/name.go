package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"trackferry/internal/online"
)

// baseName is the file name of a song without its ending: title-artist.
func baseName(s online.Saved) string {
	title, artist := clean(s.Title, 120), clean(s.Artist, 90)
	if title == "" {
		title = "未知歌曲"
	}
	if artist == "" {
		return title
	}
	return title + "-" + artist
}

// clean makes s fit for a file name of at most most bytes, on any file system.
func clean(s string, most int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == '|':
			return '&' // between artists
		case strings.ContainsRune(`:*?"<>`, r):
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > most {
		for most > 0 && !utf8.RuneStart(s[most]) {
			most--
		}
		s = s[:most]
	}
	return strings.Trim(s, " .")
}

// audioExts are the endings a kept song may carry, newest content first.
var audioExts = []string{".mp3", ".flac", ".ogg", ".wav", ".m4a"}

// freeName is a name no other file in dir has. A file of the same name and size is the same
// song, left there when the index was lost.
func freeName(dir string, s online.Saved, size int64, ext string, taken func(string) bool) string {
	base := baseName(s)
	for i := 1; ; i++ {
		name := base + ext
		if i > 1 {
			name = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		fi, err := os.Lstat(filepath.Join(dir, name))
		if !taken(name) && (os.IsNotExist(err) || err == nil && fi.Mode().IsRegular() && fi.Size() == size) {
			return name
		}
		if err != nil && !os.IsNotExist(err) { // the disk is in trouble: the rename will say so
			return name
		}
	}
}

// audioExt names the extension a song's first bytes ask for; MP3 when nothing is recognised.
func audioExt(head []byte) string {
	if len(head) >= 4 {
		switch string(head[:4]) {
		case "fLaC":
			return ".flac"
		case "OggS":
			return ".ogg"
		case "RIFF":
			return ".wav"
		}
		if len(head) >= 12 && string(head[4:8]) == "ftyp" {
			return ".m4a"
		}
	}
	return ".mp3"
}

// ownName reports whether a name read from the index stays inside the folder.
func ownName(name string) bool {
	return name == filepath.Base(name) && len(name) > len(".mp3") && slices.Contains(audioExts, filepath.Ext(name))
}
