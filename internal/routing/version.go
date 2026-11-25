package routing

import (
	"context"
	"slices"

	"trackferry/internal/intent"
	"trackferry/internal/names"
)

// Previous is the song a speaker was last asked for.
type Previous struct {
	Title  string
	Artist string
	Tried  []string // singers already played
	Track  int64    // in the library; 0 = not there
}

type prevKey struct{}

// After tells Decide what was asked for before.
func After(ctx context.Context, p Previous) context.Context {
	return context.WithValue(ctx, prevKey{}, p)
}

// version turns "by someone else" into the request it stands for.
func (r *Router) version(ctx context.Context, in intent.Intent, query string) (intent.Intent, bool) {
	prev, after := ctx.Value(prevKey{}).(Previous)
	after = after && prev.Title != ""
	var singers []string
	if after {
		for _, m := range r.Library.Song(prev.Title, "", query).Matches {
			if !slices.Contains(singers, m.Artist) {
				singers = append(singers, m.Artist)
			}
		}
	}
	if in.Artist == "" {
		for _, s := range singers {
			if !slices.ContainsFunc(prev.Tried, func(t string) bool { return names.SameArtist(t, s) }) {
				return intent.Intent{Kind: intent.KindSong, Title: prev.Title, Artist: s}, true
			}
		}
		return in, false
	}
	song := intent.Intent{Kind: intent.KindSong, Title: in.Title}
	if after {
		// What was heard may be a character off from a singer we know sings it.
		for _, near := range []bool{false, true} {
			for _, s := range singers {
				if names.ArtistMatch(s, in.Artist) || near && slices.ContainsFunc(names.Artists(s), func(n string) bool { return names.NearArtist(n, names.Norm(in.Artist)) }) {
					if !near && names.SameArtist(s, prev.Artist) {
						return intent.Intent{Kind: intent.KindArtist, Artist: in.Artist}, true // already on: more of theirs
					}
					return intent.Intent{Kind: intent.KindSong, Title: prev.Title, Artist: s}, true
				}
			}
		}
	}
	// A title that really ends in 的.
	if in.Title != "" && len(r.Library.Song(in.Title, "", query).Matches) > 0 {
		return song, true
	}
	if after {
		return intent.Intent{Kind: intent.KindSong, Title: prev.Title, Artist: in.Artist}, true
	}
	if _, n := r.Library.Artist(in.Artist); n > 0 {
		return intent.Intent{Kind: intent.KindArtist, Artist: in.Artist}, true
	}
	return song, in.Title != ""
}
