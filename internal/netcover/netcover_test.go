package netcover

import (
	"testing"

	"trackferry/internal/online"
)

func TestIDsStayOnImageHosts(t *testing.T) {
	ok := online.CoverID("https://img1.kuwo.cn/star/albumcover/500/x.jpg")
	if ok == "" {
		t.Fatal("kuwo refused")
	}
	if u, good := online.CoverURL(ok); !good || u != "https://img1.kuwo.cn/star/albumcover/500/x.jpg" {
		t.Fatal(u)
	}
	for _, u := range []string{"http://127.0.0.1/x.jpg", "https://evil.com/kuwo.cn.jpg", "https://kuwo.cn.evil.com/x", "file:///etc/passwd", "https://u@img1.kuwo.cn/x", "https://img1.kuwo.cn:8080/x"} {
		if online.CoverID(u) != "" {
			t.Errorf("%s allowed", u)
		}
	}
	if _, good := online.CoverURL(online.NetCover + "aHR0cDovLzEyNy4wLjAuMS94"); good {
		t.Error("forged id served")
	}
}
