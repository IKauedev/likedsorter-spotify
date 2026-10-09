package enrich

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Overrides são gêneros definidos pelo usuário por nome de artista; vencem
// qualquer fonte externa. Arquivo YAML simples:
//
//	artistas:
//	  Djavan: mpb
//	  Marisa Monte: [mpb, pop]
type Overrides struct {
	byName map[string][]string // chave: nome em minúsculas
	names  map[string]string   // nome original
}

type overridesFile struct {
	Artistas map[string]any `yaml:"artistas"`
}

// LoadOverrides lê o arquivo; ausente devolve Overrides vazio.
func LoadOverrides(path string) (*Overrides, error) {
	o := &Overrides{byName: map[string][]string{}, names: map[string]string{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	var f overridesFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: YAML inválido: %w", path, err)
	}
	for name, v := range f.Artistas {
		var raw []string
		switch x := v.(type) {
		case string:
			raw = strings.Split(x, ",")
		case []any:
			for _, e := range x {
				raw = append(raw, fmt.Sprint(e))
			}
		default:
			return nil, fmt.Errorf("%s: artista %q: use um gênero ou uma lista de gêneros", path, name)
		}
		o.Set(name, raw)
	}
	return o, nil
}

// Set define (ou troca) os gêneros de um artista. Lista vazia equivale a Unset.
func (o *Overrides) Set(name string, genres []string) {
	k := strings.ToLower(strings.TrimSpace(name))
	gs := NormalizeGenres(genres, 0)
	if k == "" || len(gs) == 0 {
		delete(o.byName, k)
		delete(o.names, k)
		return
	}
	o.byName[k], o.names[k] = gs, strings.TrimSpace(name)
}

// Unset remove o override; informa se existia.
func (o *Overrides) Unset(name string) bool {
	k := strings.ToLower(strings.TrimSpace(name))
	_, ok := o.byName[k]
	delete(o.byName, k)
	delete(o.names, k)
	return ok
}

// Has informa se o artista tem override.
func (o *Overrides) Has(name string) bool {
	_, ok := o.byName[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// Len é o número de artistas com override.
func (o *Overrides) Len() int { return len(o.byName) }

// Entries devolve (nome, gêneros) ordenados por nome.
func (o *Overrides) Entries() [][2]string {
	keys := make([]string, 0, len(o.byName))
	for k := range o.byName {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]string, len(keys))
	for i, k := range keys {
		out[i] = [2]string{o.names[k], strings.Join(o.byName[k], ", ")}
	}
	return out
}

// Apply sobrescreve os gêneros em infos (por nome do artista) e devolve quantos mudaram.
func (o *Overrides) Apply(infos map[string]Info) int {
	n := 0
	for id, inf := range infos {
		gs, ok := o.byName[strings.ToLower(strings.TrimSpace(inf.Name))]
		if !ok {
			continue
		}
		inf.Genres, inf.Source = gs, "override"
		infos[id] = inf
		n++
	}
	return n
}

// Save grava o arquivo (ordenado, legível).
func (o *Overrides) Save(path string, write func(path string, data []byte) error) error {
	m := map[string]any{}
	for _, e := range o.Entries() {
		m[e[0]] = strings.Split(e[1], ", ")
	}
	b, err := yaml.Marshal(overridesFile{Artistas: m})
	if err != nil {
		return err
	}
	header := "# Gêneros definidos por você; vencem Spotify/MusicBrainz/Last.fm.\n# Gerencie com: likedsorter genre set|unset|list\n"
	return write(path, append([]byte(header), b...))
}
