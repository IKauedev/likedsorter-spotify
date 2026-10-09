package spotify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// MaxItemsPerRequest é o limite do Spotify de itens por chamada de escrita.
const MaxItemsPerRequest = 100

// Este arquivo concentra as ÚNICAS escritas que a ferramenta faz. Não existe
// (de propósito) método para apagar/deixar de seguir playlists.

// CreatePlaylist cria uma playlist vazia (POST /me/playlists).
func (c *Client) CreatePlaylist(ctx context.Context, name, description string, public bool) (*Playlist, error) {
	body := map[string]any{"name": name, "description": description, "public": public, "collaborative": false}
	var raw struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		SnapshotID string `json:"snapshot_id"`
		Owner      struct {
			ID string `json:"id"`
		} `json:"owner"`
	}
	if err := c.do(ctx, http.MethodPost, "/me/playlists", nil, body, &raw, false); err != nil {
		return nil, err
	}
	if raw.ID == "" {
		return nil, fmt.Errorf("spotify: criar playlist: resposta sem id")
	}
	return &Playlist{ID: raw.ID, Name: raw.Name, OwnerID: raw.Owner.ID, Public: public, SnapshotID: raw.SnapshotID, Description: description}, nil
}

func checkBatch(uris []string, allowEmpty bool) error {
	if len(uris) > MaxItemsPerRequest {
		return fmt.Errorf("spotify: lote de %d itens excede o máximo de %d", len(uris), MaxItemsPerRequest)
	}
	if len(uris) == 0 && !allowEmpty {
		return fmt.Errorf("spotify: lote vazio")
	}
	return nil
}

func itemsPath(id string) string { return "/playlists/" + url.PathEscape(id) + "/items" }

// AddItems acrescenta até 100 URIs ao fim da playlist (POST /playlists/{id}/items).
// Falhas 5xx/de rede NÃO são repetidas aqui (veja IsTransient).
func (c *Client) AddItems(ctx context.Context, id string, uris []string) error {
	if err := checkBatch(uris, false); err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, itemsPath(id), nil, map[string]any{"uris": uris}, nil, false)
}

// RemoveItems remove até 100 URIs (todas as ocorrências de cada uma)
// (DELETE /playlists/{id}/items).
func (c *Client) RemoveItems(ctx context.Context, id string, uris []string) error {
	if err := checkBatch(uris, false); err != nil {
		return err
	}
	items := make([]map[string]string, len(uris))
	for i, u := range uris {
		items[i] = map[string]string{"uri": u}
	}
	return c.do(ctx, http.MethodDelete, itemsPath(id), nil, map[string]any{"items": items}, nil, false)
}

// ReplaceItems substitui TODO o conteúdo pela lista (até 100 URIs); lista vazia
// esvazia a playlist (PUT /playlists/{id}/items). Itens além de 100 devem ser
// acrescentados depois com AddItems.
func (c *Client) ReplaceItems(ctx context.Context, id string, uris []string) error {
	if err := checkBatch(uris, true); err != nil {
		return err
	}
	if uris == nil {
		uris = []string{} // JSON "[]", não null
	}
	return c.do(ctx, http.MethodPut, itemsPath(id), nil, map[string]any{"uris": uris}, nil, false)
}
