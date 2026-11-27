package library

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"trackferry/internal/audio"
	"trackferry/internal/names"
	"trackferry/internal/store"
)

// LocalCover starts the cover IDs of the music folder: the rest names, in base64url, the file
// the picture comes from (the song itself, or an image next to it).
const LocalCover = "local-"

// NetCover starts a cover found on a platform for a song that has none (online.NetCover).
const NetCover = "net-"

// filesFormat is what the music folder's rows hold. A scan rereads every file once when the
// stored rows are older, so albums and covers reach libraries scanned before they existed.
const (
	settingFilesFormat = "library.files_format"
	filesFormat        = "2"
)

// Image names, lower case without extension, a folder's cover may have, best first.
var coverRank = map[string]int{"cover": 1, "folder": 2, "front": 3, "album": 4, "albumart": 5}
var coverExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

func localCoverID(rel string) string {
	return LocalCover + base64.RawURLEncoding.EncodeToString([]byte(rel))
}

// localAlbumID groups the songs of one album: same root, same album name, and the same
// album artist when the files say it, else the same folder (so two "Greatest Hits" stay apart).
func localAlbumID(root, rel, album, albumArtist string) string {
	if names.Norm(album) == "" {
		return ""
	}
	by := names.Norm(albumArtist)
	if by == "" {
		by = "\x00" + path.Dir(rel)
	}
	h := sha1.Sum([]byte(root + "\x00" + names.Norm(album) + "\x00" + by))
	return LocalCover + hex.EncodeToString(h[:10])
}

// folderCovers finds, once per folder and scan, the image that is the folder's cover.
type folderCovers struct {
	dir   string
	known map[string]string
}

func (c *folderCovers) of(rel string) string {
	d := path.Dir(rel)
	if v, ok := c.known[d]; ok {
		return v
	}
	v := ""
	if entries, err := os.ReadDir(filepath.Join(c.dir, filepath.FromSlash(d))); err == nil {
		best := 99
		for _, e := range entries {
			if e.IsDir() || !e.Type().IsRegular() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			base := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			rank := coverRank[base]
			if !coverExts[ext] || rank == 0 {
				continue
			}
			if rank < best {
				best, v = rank, localCoverID(path.Join(d, e.Name()))
			}
		}
	}
	c.known[d] = v
	return v
}

// localCover picks a song's cover: its own picture, else its folder's image.
func (c *folderCovers) cover(t store.TrackRow, embedded bool) string {
	if embedded {
		return localCoverID(t.Path)
	}
	return c.of(t.Path)
}

// ErrNoCover means there is no such cover.
var ErrNoCover = errors.New("no such cover")

// Cover reads a cover of the music folder. Only IDs a song of the index uses are served, so
// the ID cannot name any other file.
func (l *Library) Cover(id string) (mime string, data []byte, err error) {
	p, err := l.coverSource(id)
	if err != nil {
		return "", nil, err
	}
	ext := strings.ToLower(filepath.Ext(p))
	if formats[ext] != "" {
		return audio.Picture(p, ext)
	}
	if !coverExts[ext] {
		return "", nil, ErrNoCover
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 16<<20 {
		return "", nil, ErrNoCover
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", nil, err
	}
	mime = http.DetectContentType(b)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return mime, b, nil
	}
	return "", nil, ErrNoCover
}

// safeRel holds for a clean path inside the music folder.
func safeRel(rel string) bool {
	return rel != "" && rel != "." && !path.IsAbs(rel) && path.Clean(rel) == rel && rel != ".." && !strings.HasPrefix(rel, "../")
}
