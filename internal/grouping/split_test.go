package grouping

import (
	"fmt"
	"testing"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/enrich"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

type byConst string

func (byConst) Name() string                                 { return "const" }
func (b byConst) Keys(spotify.SavedTrack, *Context) []string { return []string{string(b)} }

func mkTracks(n int, date string) []spotify.SavedTrack {
	var out []spotify.SavedTrack
	for i := 0; i < n; i++ {
		out = append(out, spotify.SavedTrack{ID: fmt.Sprintf("%s-%d", date, i), Name: "t",
			Album: spotify.Album{ReleaseDate: date}, AddedAt: time.Unix(int64(i), 0)})
	}
	return out
}

func TestSplitLargeByDecade(t *testing.T) {
	var tracks []spotify.SavedTrack
	tracks = append(tracks, mkTracks(30, "1995")...)
	tracks = append(tracks, mkTracks(25, "2005")...)
	tracks = append(tracks, mkTracks(3, "2015")...) // pequeno: vai para "Rock · Outros"
	res, err := Build(tracks, map[string]enrich.Info{}, byConst("Rock"), Options{SplitOver: 40})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, g := range res.Groups {
		got[g.Key] = len(g.Tracks)
	}
	want := map[string]int{"Rock · Anos 90": 30, "Rock · Anos 2000": 25, "Rock · Outros": 3}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q = %d, quero %d (todos: %v)", k, got[k], v, got)
		}
	}
}

func TestSplitLargeNoopWhenSmallOrSingleBucket(t *testing.T) {
	tracks := mkTracks(60, "1995") // tudo na mesma década: dividir não separa nada
	res, _ := Build(tracks, nil, byConst("Rock"), Options{SplitOver: 40})
	if len(res.Groups) != 1 || res.Groups[0].Key != "Rock" {
		t.Errorf("deveria manter o grupo: %+v", res.Groups)
	}
	res, _ = Build(mkTracks(10, "1995"), nil, byConst("Rock"), Options{SplitOver: 40})
	if len(res.Groups) != 1 {
		t.Error("grupo pequeno não deve ser dividido")
	}
}

func TestSplitOptionsValidation(t *testing.T) {
	if _, err := Build(nil, nil, byConst("x"), Options{SplitBy: "mes"}); err == nil {
		t.Error("split-by inválido deveria falhar")
	}
	if _, err := Build(nil, nil, byConst("x"), Options{SplitOver: -1}); err == nil {
		t.Error("split-over negativo deveria falhar")
	}
}
