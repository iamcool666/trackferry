package names

import (
	"strings"
	"unicode"
)

// counts and denials make another title of the same words: 第二次 is not 第三次.
const (
	counts  = "零一二三四五六七八九十百千万两几"
	denials = "不没別别无非未"
)

// NearTitle reports whether two titles, as Norm leaves them, differ by one character that was
// misheard, left out or added. Equal ones are not near. With the singer known, three characters
// are enough to go by; without, it takes four.
func NearTitle(a, b string, singer bool) bool {
	least := 4
	if singer {
		least = 3
	}
	return near(a, b, least, true)
}

// NearArtist reports whether two singers' names, as Norm leaves them, differ by one misheard
// character.
func NearArtist(a, b string) bool { return near(a, b, 3, false) }

// CreditNear reports whether a credit includes a singer whose name may have been misheard as want.
func CreditNear(credit, want string) bool {
	w := Norm(want)
	for _, a := range Artists(credit) {
		if NearArtist(a, w) {
			return true
		}
	}
	return false
}

func near(a, b string, least int, grow bool) bool {
	x, y := []rune(a), []rune(b)
	if len(x) < len(y) {
		x, y = y, x
	}
	if len(x)-len(y) > 1 || len(x) != len(y) && !grow || len(y) < least {
		return false
	}
	i, j := 0, 0
	for i < len(y) && x[i] == y[i] {
		i++
	}
	for j < len(y)-i && x[len(x)-1-j] == y[len(y)-1-j] {
		j++
	}
	dx, dy := x[i:len(x)-j], y[i:len(y)-j]
	if len(dx) != 1 || len(dy) > 1 { // equal, or more than one apart
		return false
	}
	latin := false
	for _, r := range append(append([]rune{}, dx...), dy...) {
		if unicode.IsDigit(r) || strings.ContainsRune(denials, r) {
			return false
		}
		latin = latin || !unicode.Is(unicode.Han, r)
	}
	if len(dy) == 1 && strings.ContainsRune(counts, dx[0]) && strings.ContainsRune(counts, dy[0]) {
		return false
	}
	return !latin || len(y) >= 8 // a letter is far less than a character
}
