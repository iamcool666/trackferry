package library

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

const maxLyricBytes = 512 << 10

func (l *Library) SidecarLyrics(trackPath string) (string, error) {
	rel := filepath.FromSlash(trackPath)
	if !filepath.IsLocal(rel) || filepath.Ext(rel) == "" {
		return "", nil
	}
	root, err := os.OpenRoot(l.dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	base := strings.TrimSuffix(rel, filepath.Ext(rel))
	for _, ext := range []string{".lrc", ".LRC"} {
		info, err := root.Stat(base + ext)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() > maxLyricBytes {
			return "", nil
		}
		file, err := root.Open(base + ext)
		if err != nil {
			return "", err
		}
		raw, err := io.ReadAll(io.LimitReader(file, maxLyricBytes+1))
		file.Close()
		if err != nil {
			return "", err
		}
		if len(raw) > maxLyricBytes {
			return "", nil
		}
		switch {
		case bytes.HasPrefix(raw, []byte{0xff, 0xfe}), bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
			raw, err = unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(raw)
		case !utf8.Valid(raw):
			raw, err = simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
		}
		return strings.TrimPrefix(string(raw), "\ufeff"), err
	}
	return "", nil
}
