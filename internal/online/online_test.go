package online

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// platform fakes both platforms and their CDN.
type platform struct {
	kuwo, migu string // search answers
	size       int    // audio bytes
	expired    bool   // the first audio address stops working
	slow       time.Duration
}

func (p *platform) start(t *testing.T) *Service {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/ks", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(p.slow)
		if p.kuwo == "500" {
			http.Error(w, "", 500)
			return
		}
		fmt.Fprint(w, p.kuwo)
	})
	mux.HandleFunc("/ms", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, p.migu) })
	n := 0
	mux.HandleFunc("/kp", func(w http.ResponseWriter, r *http.Request) {
		n++
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"url": fmt.Sprintf("%s/audio/%d", srv.URL, n), "rid": r.URL.Query().Get("rid")}})
	})
	mux.HandleFunc("/mp", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":{"cannotCode":"440006"}}`)
	})
	mux.HandleFunc("/audio/", func(w http.ResponseWriter, r *http.Request) {
		if p.expired && strings.HasSuffix(r.URL.Path, "/1") {
			http.Error(w, "", 403)
			return
		}
		b := make([]byte, p.size)
		for i := 0; i+417 <= len(b); i += 417 {
			copy(b[i:], []byte{0xff, 0xfb, 0x90, 0})
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader(string(b)))
	})
	c := srv.Client()
	return New(slog.New(slog.DiscardHandler), c, &Kuwo{Client: c, SearchURL: srv.URL + "/ks", PlayURL: srv.URL + "/kp"},
		&Migu{Client: c, SearchURL: srv.URL + "/ms", PlayURL: srv.URL + "/mp"})
}

func kuwo(items ...string) string { return `{"abslist":[` + strings.Join(items, ",") + `]}` }
func kitem(id, title, artist string, vip int) string {
	return fmt.Sprintf(`{"DC_TARGETID":"%s","SONGNAME":"%s","ARTIST":"%s","DURATION":"200","payInfo":{"feeType":{"vip":"%d"}}}`, id, title, artist, vip)
}
func mitem(title, artist string) string {
	return fmt.Sprintf(`{"contentId":"c","copyrightId":"p","songName":"%s","duration":200,"singerList":[{"name":"%s"}]}`, title, artist)
}

const full = 200 * 16000 // 200 s at 128 kbps

func TestFind(t *testing.T) {
	named := []Reading{{Title: "晴天", Artist: "周杰伦"}}
	for name, tc := range map[string]struct {
		p      platform
		rs     []Reading
		found  bool
		states [2]string
	}{
		"full":      {platform{kuwo: kuwo(kitem("1", "晴天&nbsp;", "周杰伦", 0)), migu: `[]`, size: full}, named, true, [2]string{StateHit, StateMiss}},
		"vip":       {platform{kuwo: kuwo(kitem("1", "晴天", "周杰伦", 1)), migu: `[]`, size: full}, named, false, [2]string{StateRestricted, StateMiss}},
		"trial":     {platform{kuwo: kuwo(kitem("1", "晴天", "周杰伦", 0)), migu: `[]`, size: full / 5}, named, false, [2]string{StateRestricted, StateMiss}},
		"cover":     {platform{kuwo: kuwo(kitem("1", "晴天", "别人", 0), kitem("2", "晴天 (Live)", "周杰伦", 0)), migu: `[]`, size: full}, named, false, [2]string{StateMiss, StateMiss}},
		"migu-deny": {platform{kuwo: kuwo(), migu: `[` + mitem("晴天", "周杰伦") + `]`}, named, false, [2]string{StateMiss, StateRestricted}},
		"down":      {platform{kuwo: "500", migu: `[]`}, named, false, [2]string{StateError, StateMiss}},
		"timeout":   {platform{kuwo: kuwo(), migu: `[]`, slow: 2500 * time.Millisecond}, named, false, [2]string{StateTimeout, StateMiss}},
		"disagree": {platform{kuwo: kuwo(kitem("1", "学猫叫", "甲", 0)), migu: `[` + mitem("学猫叫", "乙") + `]`, size: full}, []Reading{{Title: "学猫叫"}}, true,
			[2]string{StateHit, StateSkipped}},
		"near":    {platform{kuwo: kuwo(kitem("1", "雨中的恋人们", "黄凯芹", 0)), migu: `[]`, size: full}, []Reading{{Title: "雨中的恋人"}}, true, [2]string{StateHit, StateMiss}},
		"near by": {platform{kuwo: kuwo(kitem("1", "龙卷风", "别人", 0), kitem("2", "龙卷风", "周杰伦", 0)), migu: `[]`, size: full}, []Reading{{Title: "龙卷峰", Artist: "周杰伦"}}, true, [2]string{StateHit, StateMiss}},
		"singer":  {platform{kuwo: kuwo(kitem("1", "晴天", "周杰伦", 0)), migu: `[]`, size: full}, []Reading{{Title: "晴天", Artist: "周结伦"}}, true, [2]string{StateHit, StateMiss}},
		"exact":   {platform{kuwo: kuwo(kitem("1", "真的爱他", "甲", 0)), migu: `[` + mitem("真的爱你", "乙") + `]`, size: full}, []Reading{{Title: "真的爱你"}}, false, [2]string{StateMiss, StateRestricted}},
		"joined":  {platform{kuwo: kuwo(kitem("1", "晴天", "周杰伦", 0)), migu: `[]`, size: full}, []Reading{{Title: "周杰伦晴天"}}, true, [2]string{StateHit, StateMiss}},
	} {
		t.Run(name, func(t *testing.T) {
			f, outs := tc.p.start(t).Find(context.Background(), tc.rs, "")
			if (f != nil) != tc.found || outs[0].State != tc.states[0] || outs[1].State != tc.states[1] {
				t.Fatalf("found %v, %+v", f != nil, outs)
			}
		})
	}
}

func TestOpenerRenewsExpiredAddress(t *testing.T) {
	p := platform{kuwo: kuwo(kitem("1", "晴天", "周杰伦", 0)), migu: `[]`, size: full}
	s := p.start(t)
	f, _ := s.Find(context.Background(), []Reading{{Title: "晴天", Artist: "周杰伦"}}, "")
	p.expired = true
	resp, err := s.Opener(f)(context.Background(), "bytes=100-199")
	if err != nil || resp.StatusCode != 206 || resp.ContentLength != 100 {
		t.Fatalf("%v %v", resp, err)
	}
	resp.Body.Close()
}

func TestBreakerAndGuards(t *testing.T) {
	p := platform{kuwo: "500", migu: `[]`}
	s := p.start(t)
	for range breakAfter {
		s.Find(context.Background(), []Reading{{Title: "x"}}, "")
	}
	if _, outs := s.Find(context.Background(), []Reading{{Title: "x"}}, ""); outs[0].State != StateOpen || outs[1].State != StateMiss {
		t.Fatalf("%+v", outs)
	}
	for _, u := range []string{"http://127.0.0.1/", "http://example.com/", "http://kuwo.cn.evil.com/", "http://localhost.kuwo.cn.:80"} {
		if resp, err := Client().Get(u); err == nil {
			resp.Body.Close()
			t.Errorf("%s was allowed", u)
		}
	}
}
