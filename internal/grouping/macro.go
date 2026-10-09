package grouping

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed macro_genres.yaml
var defaultMacroYAML []byte

// DefaultMacroYAML devolve o mapa embutido (útil para o usuário copiar e editar).
func DefaultMacroYAML() []byte { return append([]byte(nil), defaultMacroYAML...) }

type macroFile struct {
	Fallback string `yaml:"fallback"`
	Families []struct {
		Name     string   `yaml:"name"`
		Priority int      `yaml:"priority"`
		Contains []string `yaml:"contains"`
		Regex    []string `yaml:"regex"`
	} `yaml:"families"`
}

type family struct {
	name     string
	priority int
	contains []string
	regex    []*regexp.Regexp
}

// MacroMap mapeia gêneros específicos para famílias.
type MacroMap struct {
	Fallback string
	families []family
}

// DefaultMacroMap carrega o mapa embutido.
func DefaultMacroMap() *MacroMap {
	m, err := ParseMacroMap(defaultMacroYAML)
	if err != nil {
		panic("macro_genres.yaml embutido inválido: " + err.Error())
	}
	return m
}

// LoadMacroMap lê e valida um arquivo YAML do usuário.
func LoadMacroMap(path string) (*MacroMap, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ler mapa de macro-gêneros: %w", err)
	}
	m, err := ParseMacroMap(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// ParseMacroMap valida o YAML: nomes obrigatórios, regex compiláveis, ao menos uma regra por família.
func ParseMacroMap(b []byte) (*MacroMap, error) {
	var f macroFile
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true) // erro em chave desconhecida (typo comum: "contain")
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("YAML inválido: %w", err)
	}
	if len(f.Families) == 0 {
		return nil, fmt.Errorf("nenhuma família definida")
	}
	m := &MacroMap{Fallback: f.Fallback}
	if m.Fallback == "" {
		m.Fallback = OtherKey
	}
	seen := map[string]bool{}
	for i, fam := range f.Families {
		name := strings.TrimSpace(fam.Name)
		if name == "" {
			return nil, fmt.Errorf("família #%d sem nome", i+1)
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("família %q duplicada", name)
		}
		seen[strings.ToLower(name)] = true
		if len(fam.Contains) == 0 && len(fam.Regex) == 0 {
			return nil, fmt.Errorf("família %q sem regras (contains/regex)", name)
		}
		out := family{name: name, priority: fam.Priority}
		for _, c := range fam.Contains {
			if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
				out.contains = append(out.contains, c)
			}
		}
		for _, r := range fam.Regex {
			re, err := regexp.Compile("(?i)" + r)
			if err != nil {
				return nil, fmt.Errorf("família %q: regex %q inválida: %w", name, r, err)
			}
			out.regex = append(out.regex, re)
		}
		m.families = append(m.families, out)
	}
	return m, nil
}

// Family devolve a família do gênero: maior prioridade vence; empate = primeira do arquivo.
func (m *MacroMap) Family(genre string) (string, bool) {
	g := strings.ToLower(strings.TrimSpace(genre))
	best, bestPri, found := "", 0, false
	for _, f := range m.families {
		if !f.matches(g) {
			continue
		}
		if !found || f.priority > bestPri {
			best, bestPri, found = f.name, f.priority, true
		}
	}
	return best, found
}

func (f family) matches(g string) bool {
	for _, c := range f.contains {
		if strings.Contains(g, c) {
			return true
		}
	}
	for _, re := range f.regex {
		if re.MatchString(g) {
			return true
		}
	}
	return false
}
