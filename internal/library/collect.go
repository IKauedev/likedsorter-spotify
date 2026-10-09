// Package library coleta as músicas curtidas aplicando a política de
// filtragem (locais, indisponíveis, sem ID, duplicatas).
package library

import (
	"context"
	"fmt"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// PageSize é o máximo permitido por GET /me/tracks.
const PageSize = 50

// TrackSource é o que a coleta precisa do cliente Spotify (mockável).
type TrackSource interface {
	SavedTracksPage(ctx context.Context, offset, limit int) (*spotify.SavedPage, error)
}

// Stats contabiliza o que foi descartado e por quê.
type Stats struct {
	Fetched     int // itens lidos da API
	Local       int // arquivos locais (não podem entrar em playlists via API)
	Unavailable int // is_playable=false no mercado do usuário
	NoID        int // sem ID (faixa removida / "track": null)
	Duplicates  int // mesmo ID já visto (mantém a curtida mais recente)
}

// Result é o resultado da coleta. Tracks vem na ordem da API (mais recente primeiro).
type Result struct {
	Tracks  []spotify.SavedTrack
	Total   int // total informado pelo Spotify na última página lida
	Stats   Stats
	Stopped bool // true se parou ao encontrar uma faixa já conhecida (StopAt)
}

// Options ajusta a coleta.
type Options struct {
	Progress func(done, total int) // chamado após cada página; pode ser nil
	// StopAt, se não nil, é chamado em cada item (do mais recente ao mais antigo);
	// ao devolver true a coleta termina sem incluir esse item. Usado no sync incremental.
	StopAt func(spotify.SavedTrack) bool
}

// Collect percorre /me/tracks até o fim. Se ctx for cancelado devolve o erro
// do contexto (sem resultado parcial).
func Collect(ctx context.Context, src TrackSource, opt Options) (*Result, error) {
	res := &Result{}
	seen := make(map[string]struct{})

	for offset := 0; ; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := src.SavedTracksPage(ctx, offset, PageSize)
		if err != nil {
			return nil, fmt.Errorf("ler curtidas (offset %d): %w", offset, err)
		}
		res.Total = page.Total

		for _, t := range page.Items {
			// Itens vêm do mais recente ao mais antigo: ao achar um já conhecido,
			// todo o resto também é. O item conhecido não conta como "lido".
			if opt.StopAt != nil && opt.StopAt(t) {
				res.Stopped = true
				break
			}
			res.Stats.Fetched++
			switch {
			case t.IsLocal:
				res.Stats.Local++
			case t.ID == "":
				res.Stats.NoID++
			case t.Unavailable:
				res.Stats.Unavailable++
			default:
				if _, dup := seen[t.ID]; dup {
					res.Stats.Duplicates++
					continue
				}
				seen[t.ID] = struct{}{}
				res.Tracks = append(res.Tracks, t)
			}
		}
		if opt.Progress != nil {
			opt.Progress(res.Stats.Fetched, page.Total)
		}

		// Página vazia com next != null não deveria ocorrer; evita laço infinito.
		if res.Stopped || !page.HasNext || len(page.Items) == 0 {
			return res, nil
		}
		// Avança pelo que veio (não por PageSize) para não pular itens.
		offset += len(page.Items)
	}
}
