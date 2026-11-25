package names

import "testing"

func TestNearTitle(t *testing.T) {
	for _, tc := range []struct {
		a, b          string
		singer, plain bool
	}{
		{"雨中的恋人", "雨中的恋人们", true, true},
		{"雨中的恋人们", "雨中的恋人", true, true},
		{"海扩天空", "海阔天空", true, true}, // misheard in the middle
		{"龙卷峰", "龙卷风", true, false},
		{"海阔天", "海阔天空", true, false},
		{"海阔天空", "海阔天空", false, false}, // equal is not near
		{"情人", "爱人", false, false},
		{"雨中的城市", "雨中的恋人", false, false},
		{"第二次拥抱", "第三次拥抱", false, false},
		{"我们不一样", "我们一样", false, false},
		{"1987年夏天", "1988年夏天", false, false},
		{"hello", "hallo", false, false},
		{"yesterdayoncemore", "yesterdayoncemoree", true, true},
		{"啊啊啊啊", "啊啊啊啊啊", true, true},
		{"", "", false, false},
	} {
		if got := NearTitle(tc.a, tc.b, true); got != tc.singer {
			t.Errorf("%q %q with the singer: %v", tc.a, tc.b, got)
		}
		if got := NearTitle(tc.a, tc.b, false); got != tc.plain {
			t.Errorf("%q %q: %v", tc.a, tc.b, got)
		}
	}
	if !CreditNear("黄凯芹/别人", "黄凯琴") || CreditNear("周深", "周申") || CreditNear("黄凯芹", "黄凯芹") {
		t.Error("singer")
	}
}
