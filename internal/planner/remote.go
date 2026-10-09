package planner

import (
	"context"
	"fmt"

	"github.com/ikauedeveloper/likedsorter/internal/grouping"
	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

const pageSize = 50 // máximo de /me/playlists e /playlists/{id}/items

// Source é o que o planner lê do Spotify (mockável).
type Source interface {
	CurrentUserID(ctx context.Context) (string, error)
	MyPlaylists(ctx context.Context, offset, limit int) (*spotify.PlaylistPage, error)
	PlaylistItems(ctx context.Context, id string, offset, limit int) (*spotify.PlaylistItemsPage, error)
}

// FetchExisting lista as playlists do usuário, marca as gerenciadas e carrega
// os itens apenas das gerenciadas cujo nome coincide com algum grupo desejado
// (as demais só precisam de contagem). known são IDs registrados como criados
// por nós (estado local), úteis se a API devolver description nula.
func FetchExisting(ctx context.Context, src Source, groups []grouping.Group, tpl string, known map[string]bool) ([]Existing, error) {
	wanted := map[string]bool{}
	for _, g := range groups {
		wanted[Name(tpl, g.Name)] = true
	}
	return fetch(ctx, src, known, func(name string) bool { return wanted[name] })
}

// FetchManaged lista as playlists gerenciadas com TODOS os itens carregados (export).
func FetchManaged(ctx context.Context, src Source, known map[string]bool) ([]Existing, error) {
	all, err := fetch(ctx, src, known, func(string) bool { return true })
	if err != nil {
		return nil, err
	}
	var out []Existing
	for _, e := range all {
		if e.Managed {
			out = append(out, e)
		}
	}
	return out, nil
}

func fetch(ctx context.Context, src Source, known map[string]bool, load func(name string) bool) ([]Existing, error) {
	me, err := src.CurrentUserID(ctx)
	if err != nil {
		return nil, fmt.Errorf("identificar usuário: %w", err)
	}

	var out []Existing
	for offset := 0; ; {
		page, err := src.MyPlaylists(ctx, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("listar playlists (offset %d): %w", offset, err)
		}
		for _, p := range page.Items {
			out = append(out, Existing{
				ID: p.ID, Name: p.Name, Description: p.Description, OwnerID: p.OwnerID, Public: p.Public, Total: p.Total,
				Managed: IsManaged(p.Description, p.OwnerID, me, p.ID, known),
			})
		}
		offset += len(page.Items)
		if !page.HasNext || len(page.Items) == 0 {
			break
		}
	}

	for i := range out {
		e := &out[i]
		if !e.Managed || !load(e.Name) {
			continue
		}
		if err := loadItems(ctx, src, e); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// LoadItems recarrega os itens de uma playlist (usado para retomar após falha).
func LoadItems(ctx context.Context, src Source, e *Existing) error {
	e.Items, e.LocalItems = nil, 0
	return loadItems(ctx, src, e)
}

func loadItems(ctx context.Context, src Source, e *Existing) error {
	for offset := 0; ; {
		page, err := src.PlaylistItems(ctx, e.ID, offset, pageSize)
		if err != nil {
			return fmt.Errorf("ler itens de %q: %w", e.Name, err)
		}
		for _, it := range page.Items {
			switch {
			case it.IsLocal:
				e.LocalItems++
			case it.URI != "":
				e.Items = append(e.Items, Member{URI: it.URI, Type: it.Type})
			}
		}
		offset += len(page.Items)
		if !page.HasNext || len(page.Items) == 0 {
			break
		}
	}
	e.Loaded = true
	return nil
}
