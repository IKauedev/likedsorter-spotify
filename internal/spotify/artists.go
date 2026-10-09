package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// ArtistDetail é o que usamos de GET /artists/{id}. Em fev/2026 o endpoint em
// lote (/artists?ids=) foi removido, então o enriquecimento é um a um.
// "genres" está marcado como deprecated e pode vir vazio.
type ArtistDetail struct {
	ID       string
	Name     string
	Genres   []string
	ImageURL string // maior imagem com largura <= 320px (ou a primeira)
}

// Artist consulta GET /artists/{id}. Devolve *APIError com Status 404 se não existir.
func (c *Client) Artist(ctx context.Context, id string) (*ArtistDetail, error) {
	var raw struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Genres []string `json:"genres"`
		Images []struct {
			URL   string `json:"url"`
			Width int    `json:"width"`
		} `json:"images"`
	}
	if err := c.get(ctx, "/artists/"+url.PathEscape(id), nil, &raw); err != nil {
		return nil, err
	}
	d := &ArtistDetail{ID: raw.ID, Name: raw.Name, Genres: raw.Genres}
	best := -1
	for i, im := range raw.Images {
		if im.Width <= 320 && (best < 0 || im.Width > raw.Images[best].Width) {
			best = i
		}
	}
	if best < 0 && len(raw.Images) > 0 {
		best = 0
	}
	if best >= 0 {
		d.ImageURL = raw.Images[best].URL
	}
	return d, nil
}

// IsNotFound informa se err (ou algo que ele embrulha) é um 404 da API.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}
