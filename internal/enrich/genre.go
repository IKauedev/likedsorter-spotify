// Package enrich completa os metadados dos artistas (gêneros, imagem) usando
// o Spotify e, quando faltar gênero, fontes externas opcionais.
package enrich

import (
	"fmt"
	"strings"
)

// NoGenre é o nome do bucket para artistas sem nenhum gênero encontrado.
const NoGenre = "Sem gênero"

// Nomes das fontes aceitas em --genre-source.
const (
	SourceSpotify     = "spotify"
	SourceMusicBrainz = "musicbrainz"
	SourceLastFM      = "lastfm"
	SourceDeezer      = "deezer"
	SourceDiscogs     = "discogs"
	SourceNone        = "none" // Info.Source quando nenhuma fonte achou gênero
)

// ParseSources valida e normaliza "spotify,musicbrainz,lastfm" (a ordem é a prioridade).
func ParseSources(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		switch p {
		case SourceSpotify, SourceMusicBrainz, SourceLastFM, SourceDeezer, SourceDiscogs:
		default:
			return nil, fmt.Errorf("fonte de gênero desconhecida %q (use spotify, musicbrainz, lastfm, deezer, discogs)", p)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--genre-source vazio")
	}
	return out, nil
}

// noiseTags são tags de usuário que não descrevem gênero.
var noiseTags = map[string]bool{
	"seen live": true, "favorites": true, "favourites": true, "favorite": true, "favourite": true,
	"awesome": true, "love": true, "spotify": true, "under 2000 listeners": true,
	"female vocalists": true, "male vocalists": true, "female vocalist": true, "male vocalist": true,
	"american": true, "british": true, "brazilian": true, "brasil": true, "brazil": true, "usa": true, "uk": true,
	"all": true, "good": true, "best": true, "my music": true,
}

// NormalizeGenres põe em minúsculas, tira espaços, remove duplicatas e ruído,
// e limita a max itens preservando a ordem.
func NormalizeGenres(in []string, max int) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range in {
		g = strings.ToLower(strings.TrimSpace(g))
		if g == "" || noiseTags[g] || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
		if max > 0 && len(out) == max {
			break
		}
	}
	return out
}
