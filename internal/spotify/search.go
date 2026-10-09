package spotify

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// TrackInfo é o resumo de uma faixa do catálogo (busca ou consulta direta).
type TrackInfo struct {
	ID      string
	URI     string
	Name    string
	Artists []string
	Album   string
	Year    string
}

// Label devolve "Artista, Outro: Título (Álbum, 2020)".
func (t TrackInfo) Label() string {
	s := t.Name
	if len(t.Artists) > 0 {
		s = joinNames(t.Artists) + ": " + t.Name
	}
	if t.Album != "" {
		s += " (" + t.Album
		if t.Year != "" {
			s += ", " + t.Year
		}
		s += ")"
	}
	return s
}

func joinNames(ns []string) string {
	out := ""
	for i, n := range ns {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

// MaxSearchLimit é o teto de resultados por busca imposto pelo Spotify desde fev/2026.
const MaxSearchLimit = 10

type rawTrack struct {
	ID      *string `json:"id"`
	URI     string  `json:"uri"`
	Name    string  `json:"name"`
	IsLocal bool    `json:"is_local"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name        string `json:"name"`
		ReleaseDate string `json:"release_date"`
	} `json:"album"`
}

func (r rawTrack) info() TrackInfo {
	t := TrackInfo{ID: deref(r.ID), URI: r.URI, Name: r.Name, Album: r.Album.Name}
	if len(r.Album.ReleaseDate) >= 4 {
		t.Year = r.Album.ReleaseDate[:4]
	}
	for _, a := range r.Artists {
		t.Artists = append(t.Artists, a.Name)
	}
	return t
}

// SearchTracks busca faixas por texto (GET /search?type=track). limit é limitado a MaxSearchLimit.
func (c *Client) SearchTracks(ctx context.Context, query string, limit int) ([]TrackInfo, error) {
	if limit <= 0 || limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	q := url.Values{"q": {query}, "type": {"track"}, "limit": {strconv.Itoa(limit)}}
	c.addMarket(q)
	var raw struct {
		Tracks struct {
			Items []rawTrack `json:"items"`
		} `json:"tracks"`
	}
	if err := c.get(ctx, "/search", q, &raw); err != nil {
		return nil, err
	}
	out := make([]TrackInfo, 0, len(raw.Tracks.Items))
	for _, it := range raw.Tracks.Items {
		if it.ID == nil || it.IsLocal {
			continue
		}
		out = append(out, it.info())
	}
	return out, nil
}

// Track consulta uma faixa pelo ID (GET /tracks/{id}).
func (c *Client) Track(ctx context.Context, id string) (*TrackInfo, error) {
	var raw rawTrack
	q := url.Values{}
	c.addMarket(q)
	if err := c.get(ctx, "/tracks/"+url.PathEscape(id), q, &raw); err != nil {
		return nil, err
	}
	if raw.ID == nil {
		return nil, fmt.Errorf("spotify: faixa %s sem id na resposta", id)
	}
	t := raw.info()
	return &t, nil
}

// addMarket envia o market só quando é um código de país: "from_token" exigiria o
// escopo user-read-private, que a ferramenta não pede (busca e faixas são públicas).
func (c *Client) addMarket(q url.Values) {
	if c.opt.Market != "" && c.opt.Market != "from_token" {
		q.Set("market", c.opt.Market)
	}
}
