package spotify

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Artist é a referência a um artista dentro de uma faixa.
type Artist struct {
	ID   string `json:"id"` // vazio em faixas locais
	Name string `json:"name"`
}

// Album guarda o que o agrupamento por data de lançamento precisa.
type Album struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ReleaseDate     string `json:"release_date"`      // "2020", "2020-05" ou "2020-05-17"
	ReleaseDatePrec string `json:"release_precision"` // year | month | day
}

// SavedTrack é uma curtida já normalizada.
type SavedTrack struct {
	ID          string    `json:"id"`
	URI         string    `json:"uri"`
	Name        string    `json:"name"`
	Artists     []Artist  `json:"artists"`
	Album       Album     `json:"album"`
	ISRC        string    `json:"isrc,omitempty"`
	DurationMs  int       `json:"duration_ms"`
	Explicit    bool      `json:"explicit,omitempty"`
	AddedAt     time.Time `json:"added_at"`
	IsLocal     bool      `json:"is_local,omitempty"`
	Unavailable bool      `json:"unavailable,omitempty"` // is_playable=false no mercado do usuário
}

// SavedPage é uma página de /me/tracks.
type SavedPage struct {
	Items   []SavedTrack
	Total   int
	HasNext bool
}

type rawSavedPage struct {
	Total int     `json:"total"`
	Next  *string `json:"next"`
	Items []struct {
		AddedAt time.Time `json:"added_at"`
		Track   *struct {
			ID         *string `json:"id"`
			URI        string  `json:"uri"`
			Name       string  `json:"name"`
			IsLocal    bool    `json:"is_local"`
			IsPlayable *bool   `json:"is_playable"`
			DurationMs int     `json:"duration_ms"`
			Explicit   bool    `json:"explicit"`
			External   struct {
				ISRC string `json:"isrc"`
			} `json:"external_ids"`
			Artists []struct {
				ID   *string `json:"id"`
				Name string  `json:"name"`
			} `json:"artists"`
			Album struct {
				ID          *string `json:"id"`
				Name        string  `json:"name"`
				ReleaseDate string  `json:"release_date"`
				Precision   string  `json:"release_date_precision"`
			} `json:"album"`
		} `json:"track"`
	} `json:"items"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SavedTracksPage busca uma página de GET /me/tracks (limit máx. 50) com o
// market configurado no Client. Itens com "track": null (faixa removida) são
// devolvidos com ID vazio para que quem coleta decida a política.
func (c *Client) SavedTracksPage(ctx context.Context, offset, limit int) (*SavedPage, error) {
	q := url.Values{
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
		"market": {c.opt.Market},
	}
	var raw rawSavedPage
	if err := c.get(ctx, "/me/tracks", q, &raw); err != nil {
		return nil, err
	}

	page := &SavedPage{Total: raw.Total, HasNext: raw.Next != nil, Items: make([]SavedTrack, 0, len(raw.Items))}
	for _, it := range raw.Items {
		st := SavedTrack{AddedAt: it.AddedAt}
		if t := it.Track; t != nil {
			st.ID, st.URI, st.Name = deref(t.ID), t.URI, t.Name
			st.IsLocal, st.DurationMs, st.Explicit = t.IsLocal, t.DurationMs, t.Explicit
			st.Unavailable = t.IsPlayable != nil && !*t.IsPlayable
			st.ISRC = t.External.ISRC
			for _, a := range t.Artists {
				st.Artists = append(st.Artists, Artist{ID: deref(a.ID), Name: a.Name})
			}
			st.Album = Album{ID: deref(t.Album.ID), Name: t.Album.Name, ReleaseDate: t.Album.ReleaseDate, ReleaseDatePrec: t.Album.Precision}
		}
		page.Items = append(page.Items, st)
	}
	return page, nil
}
