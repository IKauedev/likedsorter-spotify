package grouping

import (
	"errors"
	"fmt"
	"time"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// SkipKey faz uma estratégia deixar a faixa fora de qualquer grupo (nem "Outros").
const SkipKey = "\x00skip"

// PlayStats é o que a estratégia "listening" precisa do histórico de reproduções.
type PlayStats interface {
	HasHistory() bool
	FirstPlay() time.Time
	Count(id string, since time.Time) int
	TopIDs(since time.Time, n int) []string
}

// Padrões do modo "listening".
const (
	DefaultListenTop     = 50
	DefaultListenDays    = 30
	DefaultForgottenDays = 180
)

// Nomes dos grupos de "listening" (o dos mais ouvidos recentes inclui o período).
const (
	GroupTopAllTime    = "Mais ouvidas de sempre"
	GroupForgotten     = "Esquecidas"
	GroupRediscovered  = "Redescobertas"
	groupTopRecentFmt  = "Mais ouvidas · %d dias"
	minRediscoverPlays = 2
)

type listening struct {
	stats          PlayStats
	now            time.Time
	top            int
	days, forgotta int
	topRecent      map[string]bool
	topAll         map[string]bool
	recentName     string
	canForgotten   bool
}

func newListening(opt Options) (Strategy, error) {
	if opt.Plays == nil || !opt.Plays.HasHistory() {
		return nil, errors.New("--by=listening precisa do histórico de reproduções: rode `likedsorter history sync` " +
			"(acumula as tocadas recentemente a cada execução) ou `likedsorter history import ARQUIVO.json` (exportação do Spotify)")
	}
	l := &listening{stats: opt.Plays, now: opt.Now, top: opt.ListenTop, days: opt.ListenDays, forgotta: opt.ForgottenDays}
	if l.now.IsZero() {
		l.now = time.Now()
	}
	if l.top <= 0 {
		l.top = DefaultListenTop
	}
	if l.days <= 0 {
		l.days = DefaultListenDays
	}
	if l.forgotta <= 0 {
		l.forgotta = DefaultForgottenDays
	}
	if l.forgotta <= l.days {
		return nil, fmt.Errorf("--forgotten-days (%d) precisa ser maior que --listen-days (%d)", l.forgotta, l.days)
	}
	l.recentName = fmt.Sprintf(groupTopRecentFmt, l.days)
	l.topRecent = toSet(l.stats.TopIDs(l.since(l.days), l.top))
	l.topAll = toSet(l.stats.TopIDs(time.Time{}, l.top))
	// "Esquecida" só faz sentido se o histórico já cobre todo o período: senão toda faixa pareceria esquecida.
	l.canForgotten = !l.stats.FirstPlay().After(l.since(l.forgotta))
	return l, nil
}

func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func (l *listening) since(days int) time.Time { return l.now.AddDate(0, 0, -days) }

func (*listening) Name() string { return "listening" }

func (l *listening) Keys(t spotify.SavedTrack, _ *Context) []string {
	var keys []string
	if l.topRecent[t.ID] {
		keys = append(keys, l.recentName)
	}
	if l.topAll[t.ID] {
		keys = append(keys, GroupTopAllTime)
	}
	oldLike := !t.AddedAt.IsZero() && t.AddedAt.Before(l.since(l.forgotta))
	if l.canForgotten && oldLike {
		recent := l.stats.Count(t.ID, l.since(l.days))
		inWindow := l.stats.Count(t.ID, l.since(l.forgotta))
		switch {
		case inWindow == 0:
			keys = append(keys, GroupForgotten)
		case recent >= minRediscoverPlays && recent == inWindow:
			keys = append(keys, GroupRediscovered)
		}
	}
	if len(keys) == 0 {
		return []string{SkipKey}
	}
	return keys
}
