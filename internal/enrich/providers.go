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

// GenreProvider busca gêneros de um artista pelo nome. Devolver (nil, nil)
// significa "artista não encontrado / sem tags", e não é erro.
type GenreProvider interface {
	Name() string
	Genres(ctx context.Context, artist string) ([]string, error)
}

const maxGenres = 3

// ---------------------------------------------------------------- MusicBrainz

// MusicBrainz usa a busca de artistas (Web Service v2). Termos: no máximo
// 1 req/s por IP e User-Agent "app/versão ( contato )" (senão, 503).
type MusicBrainz struct {
	http    *httpJSON
	baseURL string
}

// MusicBrainzOptions configura o provider; só UserAgent é obrigatório.
type MusicBrainzOptions struct {
	BaseURL   string // padrão https://musicbrainz.org/ws/2
	UserAgent string // ex.: "likedsorter/1.0 ( https://github.com/você/likedsorter )"
	HTTP      *http.Client
	Limiter   *rate.Limiter
	Sleep     func(context.Context, time.Duration) error
}

func NewMusicBrainz(o MusicBrainzOptions) (*MusicBrainz, error) {
	if strings.TrimSpace(o.UserAgent) == "" || !strings.Contains(o.UserAgent, "(") {
		return nil, errors.New("MusicBrainz exige User-Agent no formato \"app/versão ( contato )\"")
	}
	if o.BaseURL == "" {
		o.BaseURL = "https://musicbrainz.org/ws/2"
	}
	return &MusicBrainz{baseURL: o.BaseURL, http: newHTTPJSON(o.HTTP, o.UserAgent, o.Limiter, rate.Every(1100*time.Millisecond), o.Sleep)}, nil
}

func (m *MusicBrainz) Name() string { return SourceMusicBrainz }

func (m *MusicBrainz) Genres(ctx context.Context, artist string) ([]string, error) {
	q := url.Values{
		"query": {`artist:"` + luceneEscape(artist) + `"`},
		"fmt":   {"json"},
		"limit": {"5"},
	}
	var resp struct {
		Artists []struct {
			Name  string `json:"name"`
			Score int    `json:"score"`
			Tags  []struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			} `json:"tags"`
		} `json:"artists"`
	}
	if err := m.http.get(ctx, m.baseURL+"/artist?"+q.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("musicbrainz: %w", err)
	}
	// Só aceita correspondência forte (nome igual e score alto) para não
	// atribuir gêneros de um homônimo qualquer.
	for _, a := range resp.Artists {
		if a.Score < 90 || !strings.EqualFold(a.Name, artist) {
			continue
		}
		sort.SliceStable(a.Tags, func(i, j int) bool { return a.Tags[i].Count > a.Tags[j].Count })
		var names []string
		for _, t := range a.Tags {
			if t.Count > 0 {
				names = append(names, t.Name)
			}
		}
		return NormalizeGenres(names, maxGenres), nil
	}
	return nil, nil
}

func luceneEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// -------------------------------------------------------------------- Last.fm

// LastFM usa artist.getTopTags. Uso não comercial; exige API key e atribuição.
type LastFM struct {
	http    *httpJSON
	baseURL string
	apiKey  string
}

type LastFMOptions struct {
	BaseURL   string // padrão https://ws.audioscrobbler.com/2.0/
	APIKey    string
	UserAgent string
	HTTP      *http.Client
	Limiter   *rate.Limiter
	Sleep     func(context.Context, time.Duration) error
}

func NewLastFM(o LastFMOptions) (*LastFM, error) {
	if strings.TrimSpace(o.APIKey) == "" {
		return nil, errors.New("chave do Last.fm ausente: defina LASTFM_API_KEY")
	}
	if o.BaseURL == "" {
		o.BaseURL = "https://ws.audioscrobbler.com/2.0/"
	}
	return &LastFM{apiKey: o.APIKey, baseURL: o.BaseURL, http: newHTTPJSON(o.HTTP, o.UserAgent, o.Limiter, rate.Every(250*time.Millisecond), o.Sleep)}, nil
}

func (l *LastFM) Name() string { return SourceLastFM }

func (l *LastFM) Genres(ctx context.Context, artist string) ([]string, error) {
	q := url.Values{
		"method": {"artist.gettoptags"}, "artist": {artist}, "autocorrect": {"1"},
		"api_key": {l.apiKey}, "format": {"json"},
	}
	var resp struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
		TopTags struct {
			Tag []struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			} `json:"tag"`
		} `json:"toptags"`
	}
	if err := l.http.get(ctx, l.baseURL+"?"+q.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("lastfm: %w", redactKey(err, l.apiKey))
	}
	switch resp.Error {
	case 0:
	case 6: // artista não encontrado
		return nil, nil
	default:
		return nil, fmt.Errorf("lastfm: erro %d: %s", resp.Error, resp.Message)
	}
	var names []string
	for _, t := range resp.TopTags.Tag {
		// count é relativo (0-100); descarta tags marginais quando informado.
		if t.Count == 0 || t.Count >= 20 {
			names = append(names, t.Name)
		}
	}
	return NormalizeGenres(names, maxGenres), nil
}

// redactKey evita vazar a API key (que vai na query string) em mensagens de erro.
func redactKey(err error, key string) error {
	return errors.New(strings.ReplaceAll(err.Error(), key, "***"))
}

func newHTTPJSON(hc *http.Client, ua string, lim *rate.Limiter, def rate.Limit, sleep func(context.Context, time.Duration) error) *httpJSON {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	if lim == nil {
		lim = rate.NewLimiter(def, 1)
	}
	if sleep == nil {
		sleep = sleepCtx
	}
	return &httpJSON{hc: hc, userAgent: ua, limiter: lim, maxRetries: 3, sleep: sleep}
}
