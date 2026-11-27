package library

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"trackferry/internal/store"
)

func picture(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

// mp3 with title, artist, album, album artist and optionally a front cover.
func tagged(title, artist, album, by string, pic []byte) []byte {
	var fr bytes.Buffer
	text := func(id, v string) {
		fr.WriteString(id)
		binary.Write(&fr, binary.BigEndian, uint32(len(v)+1))
		fr.Write([]byte{0, 0, 3}) // UTF-8
		fr.WriteString(v)
	}
	text("TIT2", title)
	text("TPE1", artist)
	text("TALB", album)
	if by != "" {
		text("TPE2", by)
	}
	if pic != nil {
		body := append([]byte{0}, []byte("image/jpeg\x00")...)
		body = append(body, 3)
		body = append(body, []byte("front\x00")...)
		body = append(body, pic...)
		fr.WriteString("APIC")
		binary.Write(&fr, binary.BigEndian, uint32(len(body)))
		fr.Write([]byte{0, 0})
		fr.Write(body)
	}
	n := fr.Len()
	out := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	out = append(out, fr.Bytes()...)
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	for range 50 {
		out = append(out, frame...)
	}
	return out
}

func TestFolderAlbumsAndCovers(t *testing.T) {
	big := picture(1400, 1400)
	l, dir := newLib(t, map[string][]byte{
		"A/叶惠美/01.mp3":    tagged("晴天", "周杰伦", "叶惠美", "周杰伦", big),
		"A/叶惠美/02.mp3":    tagged("东风破", "周杰伦", "叶惠美", "周杰伦", nil),
		"A/叶惠美/cover.jpg": picture(300, 300),
		"A/Hits/x/1.mp3":  tagged("一", "甲", "Greatest Hits", "", nil),
		"A/Hits/y/1.mp3":  tagged("二", "乙", "Greatest Hits", "", nil),
		"A/散/1.mp3":       tagged("无专辑", "丙", "", "", nil),
	})
	l.SetThumbs(filepath.Join(t.TempDir(), "thumbs"))
	ctx := context.Background()
	if err := l.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	s := l.Shelf()
	if s.Albums != 3 {
		t.Fatalf("albums = %d, want 3 (one 叶惠美, two Greatest Hits in different folders)", s.Albums)
	}
	one, _ := l.ByPath("A/叶惠美/01.mp3")
	two, _ := l.ByPath("A/叶惠美/02.mp3")
	if one.AlbumID == "" || one.AlbumID != two.AlbumID {
		t.Fatalf("same album, different ids: %q %q", one.AlbumID, two.AlbumID)
	}
	if one.Cover != localCoverID("A/叶惠美/01.mp3") {
		t.Fatalf("embedded cover not used: %q", one.Cover)
	}
	if two.Cover != localCoverID("A/叶惠美/cover.jpg") {
		t.Fatalf("folder cover not used: %q", two.Cover)
	}
	if one.AddedMS == 0 {
		t.Fatal("no added time")
	}
	mime, b, err := l.SizedCover(one.Cover, 600)
	if err != nil || mime != "image/jpeg" {
		t.Fatalf("cover: %v %s", err, mime)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(b)); err != nil || cfg.Width != 600 {
		t.Fatalf("thumb width %d %v", cfg.Width, err)
	}
	// A second ask comes from disk.
	if _, again, _ := l.SizedCover(one.Cover, 600); !bytes.Equal(again, b) {
		t.Fatal("thumb not kept")
	}
	// Small folder image is served as it is.
	if _, b, err := l.SizedCover(two.Cover, 600); err != nil || len(b) == 0 {
		t.Fatalf("folder cover: %v", err)
	}
	// IDs that no song uses, or that leave the folder, are refused.
	for _, id := range []string{LocalCover + base64.RawURLEncoding.EncodeToString([]byte("../etc/passwd")), localCoverID("A/散/1.mp3"), "local-%%%"} {
		if _, _, err := l.Cover(id); err == nil {
			t.Fatalf("%q served", id)
		}
	}

	// A cover dropped into a folder later is picked up without the songs changing.
	os.WriteFile(filepath.Join(dir, "A/散/folder.png"), []byte("\x89PNG\r\n\x1a\n"), 0o644)
	if err := l.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if t3, _ := l.ByPath("A/散/1.mp3"); t3.Cover != localCoverID("A/散/folder.png") {
		t.Fatalf("new folder cover not seen: %q", t3.Cover)
	}
}

// Rows indexed before albums and covers existed are read again once.
func TestOldRowsAreReread(t *testing.T) {
	l, _ := newLib(t, map[string][]byte{"B/1.mp3": tagged("歌", "人", "专辑", "", nil)})
	ctx := context.Background()
	if err := l.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ := l.ByPath("B/1.mp3")
	row.AlbumID, row.Favorite = "", true
	if err := l.st.SaveTracks(ctx, 0, []store.TrackRow{row}, nil); err != nil {
		t.Fatal(err)
	}
	l.st.SetSetting(ctx, settingFilesFormat, "1")
	if err := l.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := l.ByPath("B/1.mp3")
	if got.AlbumID == "" || !got.Favorite || got.ID != row.ID {
		t.Fatalf("not reread or lost favourite: %+v", got)
	}
	if v, _ := l.st.Setting(ctx, settingFilesFormat); v != filesFormat {
		t.Fatalf("format = %q", v)
	}
}

func TestEmbeddedLyrics(t *testing.T) {
	song := tagged("歌", "人", "专辑", "", nil)
	// Splice a USLT frame into the tag: encoding UTF-8, language, empty description.
	body := append([]byte{3, 'c', 'h', 'i', 0}, []byte("[00:01.00]第一句\n[00:02.00]第二句")...)
	var fr bytes.Buffer
	fr.WriteString("USLT")
	binary.Write(&fr, binary.BigEndian, uint32(len(body)))
	fr.Write([]byte{0, 0})
	fr.Write(body)
	old := int(song[6])<<21 | int(song[7])<<14 | int(song[8])<<7 | int(song[9])
	n := old + fr.Len()
	tag := append([]byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}, song[10:10+old]...)
	tag = append(tag, fr.Bytes()...)
	tag = append(tag, song[10+old:]...)
	l, _ := newLib(t, map[string][]byte{"C/1.mp3": tag, "C/2.mp3": song, "C/2.lrc": []byte("[00:01.00]旁边的")})
	if got, _ := l.Lyrics("C/1.mp3"); got != "[00:01.00]第一句\n[00:02.00]第二句" {
		t.Fatalf("embedded = %q", got)
	}
	if got, _ := l.Lyrics("C/2.mp3"); got != "[00:01.00]旁边的" {
		t.Fatalf("sidecar = %q", got)
	}
	if got, _ := l.Lyrics("../x.mp3"); got != "" {
		t.Fatalf("outside = %q", got)
	}
}
