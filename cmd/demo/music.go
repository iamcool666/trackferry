package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type demoSong struct {
	artist, album, title string
	words                []string // "" is a break of 12 s with no singing; "~" one of 2 s
	noLyrics             bool
}

type demoAlbum struct {
	artist, album string
	from, to      color.RGBA // the cover; zero means no cover file
	songs         []demoSong
}

var albums = []demoAlbum{
	{"雾港乐队", "潮汐", color.RGBA{24, 58, 94, 255}, color.RGBA{120, 196, 210, 255}, []demoSong{
		{title: "灯塔", words: []string{"雾从海面慢慢升起", "灯塔在远处一明一灭", "我数着浪花回家的路", "", "潮水退去又回来", "像你没说完的那句话", "灯还亮着", "灯还亮着"}},
		{title: "退潮之后", words: []string{"沙滩上留下脚印", "风把它们一点点抹平", "~", "退潮之后", "我们都没有说再见", "", "退潮之后", "海还在原来的地方"}},
		{title: "防波堤（纯音乐）", noLyrics: true},
		{title: "夜航船", words: []string{"船灯在黑水上摇晃", "汽笛声穿过整座城", "你说远方并不远", "", "我把地图折进口袋", "跟着星星往前开", "夜航船", "别靠岸"}},
	}},
	{"林间信号", "无线电森林", color.RGBA{38, 70, 40, 255}, color.RGBA{214, 196, 120, 255}, []demoSong{
		{title: "调频九十八点六", words: []string{"收音机沙沙作响", "有人在山那边唱歌", "我转动旋钮找你的频率", "", "九十八点六", "信号刚好", "九十八点六", "你听得到吗"}},
		{title: "树洞", words: []string{"把秘密说给树洞", "它从来不回答", "~", "可是春天来的时候", "满树都开了花", "", "树洞", "替我记得"}},
		{title: "雨后蘑菇", words: []string{"雨停了", "地上冒出小小的伞", "我们蹲下来看了很久", "", "世界那么大", "这里刚刚好"}},
	}},
	{"Nova Arc", "Afterglow", color.RGBA{90, 30, 110, 255}, color.RGBA{250, 150, 90, 255}, []demoSong{
		{title: "Afterglow", words: []string{"The city hums a quiet tune", "We walk beneath a paper moon", "Hold the light before it goes", "", "Afterglow, afterglow", "Stay a little longer", "Afterglow"}},
		{title: "Static Hearts", words: []string{"Turn the dial, find the sound", "Every signal coming round", "~", "Static hearts in the dark", "Still a spark, still a spark"}},
	}},
	{"白噪", "没有封面的专辑", color.RGBA{}, color.RGBA{}, []demoSong{
		{title: "空白页", words: []string{"这一页什么也没写", "却好像什么都说了", "", "空白页", "留给明天"}},
		{title: "旧唱片", words: []string{"针落下的那一秒", "灰尘也在跳舞", "旧唱片转啊转", "转回那年夏天"}},
	}},
}

// writeMusic writes the folder: Artist/Album/NN. Artist - Title.wav, cover.jpg, .lrc beside.
func writeMusic(root string) error {
	for _, a := range albums {
		dir := filepath.Join(root, a.artist, a.album)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if a.from.A != 0 {
			if err := writeCover(filepath.Join(dir, "cover.jpg"), a.from, a.to); err != nil {
				return err
			}
		}
		for i, s := range a.songs {
			base := filepath.Join(dir, fmt.Sprintf("%02d. %s - %s", i+1, a.artist, s.title))
			lrc, secs := lyricsOf(s)
			if err := writeWAV(base+".wav", s.title, secs); err != nil {
				return err
			}
			if !s.noLyrics {
				if err := os.WriteFile(base+".lrc", []byte(lrc), 0o644); err != nil {
					return err
				}
			}
		}
	}
	// A loose song, named only by its file.
	if err := writeWAV(filepath.Join(root, "夜行者 - 午夜便利店.wav"), "午夜便利店", 75); err != nil {
		return err
	}
	fmt.Println("曲库写好了：", root)
	return nil
}

// lyricsOf times the words: an intro of 8 s, 4.5 s a line; a break ends the line before it
// with an empty timed line, as LRC files from real players do.
func lyricsOf(s demoSong) (string, float64) {
	if s.noLyrics {
		return "", 96
	}
	var b strings.Builder
	at := 8.0
	stamp := func(t float64) string { return fmt.Sprintf("[%02d:%05.2f]", int(t)/60, math.Mod(t, 60)) }
	b.WriteString("[ti:" + s.title + "]\n")
	for _, w := range s.words {
		switch w {
		case "":
			b.WriteString(stamp(at) + "\n") // the line before ends here
			at += 12
		case "~":
			b.WriteString(stamp(at) + "\n")
			at += 2 // too short a rest to show
		default:
			b.WriteString(stamp(at) + w + "\n")
			at += 4.5
		}
	}
	b.WriteString(stamp(at) + "\n")
	return b.String(), at + 10
}

const rate = 22050

// writeWAV writes a soft tune, different for every title.
func writeWAV(path, seed string, secs float64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	n := int(secs * rate)
	w := bufio.NewWriter(f)
	head := []any{[]byte("RIFF"), uint32(36 + n*2), []byte("WAVEfmt "), uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16), []byte("data"), uint32(n * 2)}
	for _, v := range head {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	h := fnv.New32a()
	h.Write([]byte(seed))
	x := h.Sum32()
	next := func() uint32 { x ^= x << 13; x ^= x >> 17; x ^= x << 5; return x }
	scale := []float64{0, 2, 4, 7, 9, 12, 14, 16}
	root := 196 * math.Pow(2, float64(next()%5)/12)
	beat := 0.32 + float64(next()%4)*0.04
	var note, bass float64
	buf := make([]byte, 2)
	for i := 0; i < n; i++ {
		t := float64(i) / rate
		k := int(t / beat)
		in := t - float64(k)*beat
		if in < 1.0/rate {
			note = root * 2 * math.Pow(2, scale[next()%uint32(len(scale))]/12)
			if k%8 == 0 {
				bass = root / 2 * math.Pow(2, scale[next()%4]/12)
			}
		}
		env := math.Exp(-in * 5)
		v := 0.22*env*math.Sin(2*math.Pi*note*t) + 0.1*math.Sin(2*math.Pi*bass*t)
		fade := math.Min(1, math.Min(t/1.5, (secs-t)/2))
		binary.LittleEndian.PutUint16(buf, uint16(int16(v*fade*32767)))
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	return w.Flush()
}

func writeCover(path string, from, to color.RGBA) error {
	const size = 600
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			t := float64(x+y) / (2 * size)
			dx, dy := float64(x-size*2/3), float64(y-size/3)
			ring := 0.5 + 0.5*math.Cos(math.Sqrt(dx*dx+dy*dy)/18)
			mix := func(a, b uint8) uint8 {
				v := float64(a)*(1-t) + float64(b)*t
				return uint8(math.Min(255, v*(0.88+0.12*ring)))
			}
			img.Set(x, y, color.RGBA{mix(from.R, to.R), mix(from.G, to.G), mix(from.B, to.B), 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 88})
}
