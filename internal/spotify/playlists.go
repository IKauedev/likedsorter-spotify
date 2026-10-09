package spotify

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Playlist é o subconjunto de GET /me/playlists que usamos.
type Playlist struct {
	ID            string
	Name          string
	Description   string // vazia se a API devolveu null (playlist "não modificada")
	OwnerID       string
	Public        bool
	Collaborative bool
	SnapshotID    string
	Total         int // faixas (items.total; tracks.total como reserva)
}

// PlaylistPage é uma página de playlists.
type PlaylistPage struct {
	Items   []Playlist
	Total   int
	HasNext bool
}

// PlaylistItem é um item de GET /playlists/{id}/items.
type PlaylistItem struct {
	ID      string // vazio se o item foi removido do catálogo (item: null)
	URI     string
	Type    string // track | episode
	IsLocal bool
	AddedAt time.Time
}

// PlaylistItemsPage é uma página de itens.
type PlaylistItemsPage struct {
	Items   []PlaylistItem
	Total   int
	HasNext bool
}

// CurrentUserID devolve o ID do usuário autenticado (GET /me).
func (c *Client) CurrentUserID(ctx context.Context) (string, error) {
	var raw struct {
		ID string `json:"id"`
	}
	if err := c.get(ctx, "/me", nil, &raw); err != nil {
		return "", err
	}
	return raw.ID, nil
}

// MyPlaylists lista as playlists do usuário (GET /me/playlists, até 50 por página).
func (c *Client) MyPlaylists(ctx context.Context, offset, limit int) (*PlaylistPage, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var raw struct {
		Total int     `json:"total"`
		Next  *string `json:"next"`
		Items []struct {
			ID            string  `json:"id"`
			Name          string  `json:"name"`
			Description   *string `json:"description"`
			Public        *bool   `json:"public"`
			Collaborative bool    `json:"collaborative"`
			SnapshotID    string  `json:"snapshot_id"`
			Owner         struct {
				ID string `json:"id"`
			} `json:"owner"`
			Items  *struct{ Total int } `json:"items"`  // nome atual (fev/2026)
			Tracks *struct{ Total int } `json:"tracks"` // nome antigo, deprecated
		} `json:"items"`
	}
	if err := c.get(ctx, "/me/playlists", q, &raw); err != nil {
		return nil, err
	}
	page := &PlaylistPage{Total: raw.Total, HasNext: raw.Next != nil}
	for _, it := range raw.Items {
		p := Playlist{ID: it.ID, Name: it.Name, OwnerID: it.Owner.ID, Collaborative: it.Collaborative, SnapshotID: it.SnapshotID}
		if it.Description != nil {
			p.Description = *it.Description
		}
		if it.Public != nil {
			p.Public = *it.Public
		}
		switch {
		case it.Items != nil:
			p.Total = it.Items.Total
		case it.Tracks != nil:
			p.Total = it.Tracks.Total
		}
		page.Items = append(page.Items, p)
	}
	return page, nil
}

// PlaylistItems lê os itens de uma playlist (GET /playlists/{id}/items, até 50
// por página). Só funciona para playlists próprias ou colaborativas (senão 403).
func (c *Client) PlaylistItems(ctx context.Context, id string, offset, limit int) (*PlaylistItemsPage, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	type obj struct {
		ID   *string `json:"id"`
		URI  string  `json:"uri"`
		Type string  `json:"type"`
	}
	var raw struct {
		Total int     `json:"total"`
		Next  *string `json:"next"`
		Items []struct {
			AddedAt *time.Time `json:"added_at"`
			IsLocal bool       `json:"is_local"`
			Item    *obj       `json:"item"`
			Track   *obj       `json:"track"` // nome antigo, deprecated
		} `json:"items"`
	}
	if err := c.get(ctx, "/playlists/"+url.PathEscape(id)+"/items", q, &raw); err != nil {
		return nil, err
	}
	page := &PlaylistItemsPage{Total: raw.Total, HasNext: raw.Next != nil}
	for _, it := range raw.Items {
		o := it.Item
		if o == nil {
			o = it.Track
		}
		pi := PlaylistItem{IsLocal: it.IsLocal}
		if it.AddedAt != nil {
			pi.AddedAt = *it.AddedAt
		}
		if o != nil {
			pi.ID, pi.URI, pi.Type = deref(o.ID), o.URI, o.Type
		}
		page.Items = append(page.Items, pi)
	}
	return page, nil
}
