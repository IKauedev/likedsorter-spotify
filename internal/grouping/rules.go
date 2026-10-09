package grouping

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ikauedeveloper/likedsorter/internal/spotify"
)

// RuleSet são as regras do usuário (--by=rules). Formato YAML:
//
//	primeira_regra: true        # padrão: a faixa entra só na primeira regra que casar
//	regras:
//	  - nome: MPB raiz
//	    artista: [djavan, marisa]   # trechos do nome (OU)
//	    genero: [mpb]               # trechos dos gêneros do artista principal (OU)
//	    titulo: [acústico]          # trechos do título (OU)
//	    lancamento: "1970-1999"     # ano ou intervalo de anos do álbum
//	    curtida_desde: 2023-01-01   # data em que você curtiu (AAAA-MM-DD)
//
// Dentro de uma regra todos os campos informados precisam casar (E).
type RuleSet struct {
	FirstMatch bool
	Rules      []Rule
}

// Rule é uma regra; campos vazios são ignorados.
type Rule struct {
	Name       string
	Artists    []string
	Genres     []string
	Titles     []string
	YearFrom   int
	YearTo     int
	AddedSince time.Time
}

type ruleYAML struct {
	Nome         string `yaml:"nome"`
	Artista      any    `yaml:"artista"`
	Genero       any    `yaml:"genero"`
	Titulo       any    `yaml:"titulo"`
	Lancamento   any    `yaml:"lancamento"`
	CurtidaDesde string `yaml:"curtida_desde"`
}

type ruleSetYAML struct {
	PrimeiraRegra *bool      `yaml:"primeira_regra"`
	Regras        []ruleYAML `yaml:"regras"`
}

// LoadRules lê e valida o arquivo de regras.
func LoadRules(path string) (*RuleSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRules(b, path)
}

// ParseRules interpreta o YAML; src só aparece nas mensagens de erro.
func ParseRules(b []byte, src string) (*RuleSet, error) {
	var y ruleSetYAML
	if err := yaml.Unmarshal(b, &y); err != nil {
		return nil, fmt.Errorf("%s: YAML inválido: %w", src, err)
	}
	if len(y.Regras) == 0 {
		return nil, fmt.Errorf("%s: nenhuma regra em \"regras:\"", src)
	}
	rs := &RuleSet{FirstMatch: y.PrimeiraRegra == nil || *y.PrimeiraRegra}
	seen := map[string]bool{}
	for i, r := range y.Regras {
		name := strings.TrimSpace(r.Nome)
		if name == "" {
			return nil, fmt.Errorf("%s: regra %d sem \"nome\"", src, i+1)
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("%s: nome de regra repetido: %q", src, name)
		}
		seen[strings.ToLower(name)] = true
		rule := Rule{
			Name: name, Artists: lowerList(r.Artista), Genres: lowerList(r.Genero), Titles: lowerList(r.Titulo),
		}
		if r.Lancamento != nil {
			from, to, err := parseYearRange(fmt.Sprint(r.Lancamento))
			if err != nil {
				return nil, fmt.Errorf("%s: regra %q: %w", src, name, err)
			}
			rule.YearFrom, rule.YearTo = from, to
		}
		if r.CurtidaDesde != "" {
			t, err := time.Parse("2006-01-02", r.CurtidaDesde)
			if err != nil {
				return nil, fmt.Errorf("%s: regra %q: curtida_desde deve ser AAAA-MM-DD", src, name)
			}
			rule.AddedSince = t
		}
		if len(rule.Artists)+len(rule.Genres)+len(rule.Titles) == 0 && rule.YearFrom == 0 && rule.AddedSince.IsZero() {
			return nil, fmt.Errorf("%s: regra %q sem nenhuma condição", src, name)
		}
		rs.Rules = append(rs.Rules, rule)
	}
	return rs, nil
}

func lowerList(v any) []string {
	var out []string
	add := func(s string) {
		for _, p := range strings.Split(s, ",") {
			if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
				out = append(out, p)
			}
		}
	}
	switch x := v.(type) {
	case string:
		add(x)
	case []any:
		for _, e := range x {
			add(fmt.Sprint(e))
		}
	}
	return out
}

func parseYearRange(s string) (from, to int, err error) {
	a, b, isRange := strings.Cut(strings.TrimSpace(s), "-")
	from, err = strconv.Atoi(strings.TrimSpace(a))
	if err != nil {
		return 0, 0, errors.New("lancamento deve ser um ano (1999) ou intervalo (1970-1999)")
	}
	to = from
	if isRange {
		if to, err = strconv.Atoi(strings.TrimSpace(b)); err != nil || to < from {
			return 0, 0, errors.New("lancamento: intervalo inválido (use 1970-1999)")
		}
	}
	return from, to, nil
}

func containsAny(hay string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

// Match informa se a faixa satisfaz todas as condições da regra.
func (r Rule) Match(t spotify.SavedTrack, genres []string) bool {
	if len(r.Artists) > 0 {
		ok := false
		for _, a := range t.Artists {
			if containsAny(strings.ToLower(a.Name), r.Artists) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(r.Genres) > 0 {
		ok := false
		for _, g := range genres {
			if containsAny(strings.ToLower(g), r.Genres) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(r.Titles) > 0 && !containsAny(strings.ToLower(t.Name), r.Titles) {
		return false
	}
	if r.YearFrom != 0 {
		if y := releaseYear(t); y < r.YearFrom || y > r.YearTo {
			return false
		}
	}
	if !r.AddedSince.IsZero() && t.AddedAt.Before(r.AddedSince) {
		return false
	}
	return true
}

// NeedsGenres informa se alguma regra consulta gêneros.
func (rs *RuleSet) NeedsGenres() bool {
	for _, r := range rs.Rules {
		if len(r.Genres) > 0 {
			return true
		}
	}
	return false
}

type rulesStrategy struct{ rs *RuleSet }

func (rulesStrategy) Name() string { return "rules" }
func (s rulesStrategy) Keys(t spotify.SavedTrack, c *Context) []string {
	genres := c.PrimaryGenres(t)
	var keys []string
	for _, r := range s.rs.Rules {
		if r.Match(t, genres) {
			keys = append(keys, r.Name)
			if s.rs.FirstMatch {
				break
			}
		}
	}
	if len(keys) == 0 {
		return []string{OtherKey}
	}
	return keys
}
