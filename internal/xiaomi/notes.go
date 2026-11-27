package xiaomi

import "strings"

// noFLAC are models that cannot decode FLAC pushed to them, as users and other speaker
// projects (xiaomusic) found: such songs are fetched and then stay silent.
var noFLAC = map[string]bool{"L05B": true, "L05C": true, "LX05": true, "LX06": true, "L16A": true}

// NoFLAC reports whether a model is known to stay silent on FLAC.
func NoFLAC(hardware string) bool { return noFLAC[strings.ToUpper(hardware)] }

// Note is something a speaker of this model does differently, shown on its card.
type Note struct {
	Kind string `json:"kind"` // flac | answer
	Text string `json:"text"`
}

// Notes lists what is known about a model's firmware that TrackFerry cannot change.
func Notes(hardware string) []Note {
	var out []Note
	h := strings.ToUpper(hardware)
	if noFLAC[h] {
		out = append(out, Note{"flac", "这个型号放不了 FLAC 等无损格式，取了流也不出声。"})
	}
	if h == "OH2P" {
		out = append(out, Note{"answer", "小爱说话时打断不了，这是固件行为，只有按实体键能停。不想听那段播报，就把“压住小爱的回答”改成“等小爱说完”，出歌会慢一点。"})
	}
	return out
}
