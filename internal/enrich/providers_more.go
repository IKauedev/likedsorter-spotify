package enrich

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// ----------------------------------------------------------------- Deezer

// Deezer usa a API pública (sem chave, ~50 req/5 s). O gênero fica no álbum:
// artista → faixas mais tocadas → álbum → genres.
type Deezer struct {
	http    *httpJSON
	baseURL string
}

// DeezerOptions configura o provider; tudo é opcional.
type DeezerOptions struct {
	BaseURL   string // padrão https://api.deezer.com
	UserAgent string
	HTTP      *http.Client
	Limiter   *rate.Limiter
	Sleep     func(context.Context, time.Duration) error
}

func NewDeezer(o DeezerOptions) *Deezer {
	if o.BaseURL == "" {
		o.BaseURL = "https://api.deezer.com"
	}
	return &Deezer{baseURL: o.BaseURL, http: newHTTPJSON(o.HTTP, o.UserAgent, o.Limiter, rate.Every(120*time.Millisecond), o.Sleep)}
}

func (d *Deezer) Name() string { return SourceDeezer }

type deezerErr struct {
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func (e deezerErr) err() error {
	if e.Error == nil {
		return nil
	}
	return fmt.Errorf("deezer: %s (%d): %s", e.Error.Type, e.Error.Code, e.Error.Message)
}

func (d *Deezer) Genres(ctx context.Context, artist string) ([]string, error) {
	var found struct {
		deezerErr
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	q := url.Values{"q": {artist}, "limit": {"5"}}
	if err := d.http.get(ctx, d.baseURL+"/search/artist?"+q.Encode(), &found); err != nil {
		return nil, fmt.Errorf("deezer: %w", err)
	}
	if err := found.err(); err != nil {
		return nil, err
	}
	var id int64
	for _, a := range found.Data { // só nome idêntico, para não pegar homônimo
		if strings.EqualFold(strings.TrimSpace(a.Name), strings.TrimSpace(artist)) {
			id = a.ID
			break
		}
	}
	if id == 0 {
		return nil, nil
	}

	var top struct {
		deezerErr
		Data []struct {
			Album struct {
				ID int64 `json:"id"`
			} `json:"album"`
		} `json:"data"`
	}
	if err := d.http.get(ctx, fmt.Sprintf("%s/artist/%d/top?limit=5", d.baseURL, id), &top); err != nil {
		return nil, fmt.Errorf("deezer: %w", err)
	}
	if err := top.err(); err != nil {
		return nil, err
	}
	count := map[string]int{}
	seen := map[int64]bool{}
	for _, t := range top.Data {
		if t.Album.ID == 0 || seen[t.Album.ID] || len(seen) == 3 {
			continue
		}
		seen[t.Album.ID] = true
		var alb struct {
			deezerErr
			Genres struct {
				Data []struct {
					Name string `json:"name"`
				} `json:"data"`
			} `json:"genres"`
		}
		if err := d.http.get(ctx, fmt.Sprintf("%s/album/%d", d.baseURL, t.Album.ID), &alb); err != nil {
			return nil, fmt.Errorf("deezer: %w", err)
		}
		if err := alb.err(); err != nil {
			return nil, err
		}
		for _, g := range alb.Genres.Data {
			count[g.Name]++
		}
	}
	return topByCount(count, "all", "todos"), nil
}

// topByCount ordena por frequência (e nome, para ser determinístico), normaliza e corta em maxGenres.
func topByCount(count map[string]int, skip ...string) []string {
	type kv struct {
		k string
		n int
	}
	var kvs []kv
	for k, n := range count {
		kvs = append(kvs, kv{k, n})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].n != kvs[j].n {
			return kvs[i].n > kvs[j].n
		}
		return kvs[i].k < kvs[j].k
	})
	names := make([]string, 0, len(kvs))
	for _, e := range kvs {
		names = append(names, e.k)
	}
	out := NormalizeGenres(names, 0)
	var filtered []string
	for _, g := range out {
		skipIt := false
		for _, s := range skip {
			skipIt = skipIt || g == s
		}
		if !skipIt {
			filtered = append(filtered, g)
		}
	}
	if len(filtered) > maxGenres {
		filtered = filtered[:maxGenres]
	}
	return filtered
}

// ---------------------------------------------------------------- Discogs

// Discogs usa /database/search (type=release): cada resultado já traz style e genre.
// Exige um token pessoal (https://www.discogs.com/settings/developers); 60 req/min.
type Discogs struct {
	http    *httpJSON
	baseURL string
}

type DiscogsOptions struct {
	BaseURL   string // padrão https://api.discogs.com
	Token     string
	UserAgent string
	HTTP      *http.Client
	Limiter   *rate.Limiter
	Sleep     func(context.Context, time.Duration) error
}

func NewDiscogs(o DiscogsOptions) (*Discogs, error) {
	if strings.TrimSpace(o.Token) == "" {
		return nil, errors.New("token do Discogs ausente: defina DISCOGS_TOKEN (https://www.discogs.com/settings/developers)")
	}
	if o.BaseURL == "" {
		o.BaseURL = "https://api.discogs.com"
	}
	h := newHTTPJSON(o.HTTP, o.UserAgent, o.Limiter, rate.Every(1100*time.Millisecond), o.Sleep)
	h.header = http.Header{"Authorization": {"Discogs token=" + strings.TrimSpace(o.Token)}}
	return &Discogs{baseURL: o.BaseURL, http: h}, nil
}

func (d *Discogs) Name() string { return SourceDiscogs }

func (d *Discogs) Genres(ctx context.Context, artist string) ([]string, error) {
	q := url.Values{"type": {"release"}, "artist": {artist}, "per_page": {"15"}}
	var resp struct {
		Results []struct {
			Title string   `json:"title"`
			Genre []string `json:"genre"`
			Style []string `json:"style"`
		} `json:"results"`
	}
	if err := d.http.get(ctx, d.baseURL+"/database/search?"+q.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("discogs: %w", err)
	}
	styles, genres := map[string]int{}, map[string]int{}
	for _, r := range resp.Results {
		// título vem como "Artista - Álbum": só conta releases cujo artista é exatamente o buscado
		name, _, ok := strings.Cut(r.Title, " - ")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(artist)) {
			continue
		}
		for _, s := range r.Style {
			styles[s]++
		}
		for _, g := range r.Genre {
			genres[g]++
		}
	}
	// estilos ("MPB", "Bossa Nova") descrevem melhor que gêneros amplos ("Latin", "Rock")
	out := topByCount(styles)
	for _, g := range topByCount(genres) {
		if len(out) >= maxGenres {
			break
		}
		dup := false
		for _, o := range out {
			dup = dup || o == g
		}
		if !dup {
			out = append(out, g)
		}
	}
	return out, nil
}
