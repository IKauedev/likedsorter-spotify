// Package planner compara os grupos desejados com as playlists gerenciadas
// que já existem no Spotify e calcula o que seria criado, atualizado ou mantido.
// Nada aqui escreve no Spotify.
package planner

import (
	"fmt"
	"strings"

	"github.com/ikauedeveloper/likedsorter/internal/grouping"
)

const (
	// Marker identifica, na descrição, as playlists que a ferramenta gerencia.
	// Só playlists com o marcador (ou criadas por nós, registradas no estado) são alteradas.
	Marker = "[managed:likedsorter]"

	DefaultNameTemplate = "Curtidas • {group}"
	maxNameRunes        = 100
	maxDescriptionRunes = 300
)

// Action é o que o plano faria com uma playlist.
type Action string

const (
	Create Action = "create" // não existe: criar e adicionar tudo
	Update Action = "update" // existe e difere do desejado
	Keep   Action = "keep"   // existe e já está igual
	Orphan Action = "orphan" // gerenciada, mas sem grupo correspondente (nunca é apagada)
)

// Existing é uma playlist do usuário já existente no Spotify.
type Existing struct {
	ID          string
	Name        string
	Description string
	OwnerID     string
	Public      bool
	Total       int
	Managed     bool
	Loaded      bool     // Items foi preenchido
	Items       []Member // só faixas "reais" (sem locais)
	LocalItems  int      // itens locais ignorados na comparação
}

// Member é uma faixa dentro de uma playlist existente.
type Member struct {
	URI  string
	Type string // track | episode
}

// Item é uma linha do plano.
type Item struct {
	Action     Action   `json:"action"`
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Part       int      `json:"part,omitempty"`
	Parts      int      `json:"parts,omitempty"`
	ExistingID string   `json:"existing_id,omitempty"`
	Desired    int      `json:"desired"`    // faixas que a playlist deve ter
	Keep       int      `json:"keep"`       // já presentes e desejadas
	Add        []string `json:"add"`        // URIs a adicionar, na ordem do grupo
	Remove     []string `json:"remove"`     // URIs presentes e não desejadas (só removidas com --allow-remove)
	Duplicates int      `json:"duplicates"` // repetições extras de faixas desejadas na playlist existente

	// DesiredURIs é o conteúdo completo e ordenado que a playlist deve ter
	// (usado pelo modo recreate). Fora do JSON do relatório.
	DesiredURIs []string `json:"-"`
}

// Plan é o resultado de Build.
type Plan struct {
	Items    []Item   `json:"items"`
	Warnings []string `json:"warnings,omitempty"`
}

// Totals soma o plano.
type Totals struct {
	Create, Update, Keep, Orphan int
	Add, Remove                  int
}

func (p Plan) Totals() Totals {
	var t Totals
	for _, it := range p.Items {
		switch it.Action {
		case Create:
			t.Create++
		case Update:
			t.Update++
		case Keep:
			t.Keep++
		case Orphan:
			t.Orphan++
		}
		t.Add += len(it.Add)
		t.Remove += len(it.Remove)
	}
	return t
}

// Input reúne o que Build precisa.
type Input struct {
	Groups       []grouping.Group
	Existing     []Existing // todas as playlists do usuário (Managed já resolvido)
	NameTemplate string
}

// Name aplica o template ao nome do grupo e limita a 100 caracteres.
func Name(tpl, group string) string {
	if tpl == "" {
		tpl = DefaultNameTemplate
	}
	n := strings.ReplaceAll(tpl, "{group}", group)
	if r := []rune(n); len(r) > maxNameRunes {
		n = string(r[:maxNameRunes-1]) + "…"
	}
	return n
}

// ValidateTemplate exige o placeholder {group}.
func ValidateTemplate(tpl string) error {
	if !strings.Contains(tpl, "{group}") {
		return fmt.Errorf("o template de nome precisa conter {group}, recebi %q", tpl)
	}
	return nil
}

// Description é o texto gravado nas playlists criadas (termina com o marcador).
func Description(by string) string {
	d := fmt.Sprintf("Gerada pelo likedsorter (--by=%s). Atualizada automaticamente; alterações manuais podem ser sobrescritas. %s", by, Marker)
	if r := []rune(d); len(r) > maxDescriptionRunes {
		// preserva o marcador no fim
		keep := maxDescriptionRunes - len([]rune(Marker)) - 2
		d = string(r[:keep]) + "… " + Marker
	}
	return d
}

// IsManaged diz se a playlist é nossa: dona do usuário e com o marcador na
// descrição, ou com ID registrado em known (a API pode devolver description
// null para playlists não modificadas).
func IsManaged(description, ownerID, me, id string, known map[string]bool) bool {
	if me != "" && ownerID != me {
		return false
	}
	return known[id] || strings.Contains(description, Marker)
}

// Build calcula o plano.
func Build(in Input) Plan {
	var plan Plan

	managedByName := map[string][]*Existing{}
	otherNames := map[string]bool{}
	for i := range in.Existing {
		e := &in.Existing[i]
		if e.Managed {
			managedByName[e.Name] = append(managedByName[e.Name], e)
		} else {
			otherNames[e.Name] = true
		}
	}

	used := map[string]bool{}
	for _, g := range in.Groups {
		name := Name(in.NameTemplate, g.Name)
		desired := make([]string, len(g.Tracks))
		for i, t := range g.Tracks {
			desired[i] = "spotify:track:" + t.ID
		}
		it := Item{Key: g.Key, Name: name, Part: g.Part, Parts: g.Parts, Desired: len(desired), DesiredURIs: desired}

		cands := managedByName[name]
		if len(cands) == 0 {
			it.Action, it.Add = Create, desired
			if otherNames[name] {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("já existe uma playlist sua chamada %q sem o marcador do likedsorter; ela não será tocada e outra será criada", name))
			}
			plan.Items = append(plan.Items, it)
			continue
		}
		if len(cands) > 1 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d playlists gerenciadas chamadas %q; usando a primeira (%s)", len(cands), name, cands[0].ID))
		}
		e := cands[0]
		used[e.ID] = true
		it.ExistingID = e.ID
		if !e.Loaded {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("itens de %q não foram carregados; comparação ignorada", name))
		}
		if e.LocalItems > 0 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%q contém %d arquivo(s) local(is), ignorado(s) na comparação", name, e.LocalItems))
		}
		diff(&it, desired, e.Items)
		if len(it.Add) == 0 && len(it.Remove) == 0 {
			it.Action = Keep
		} else {
			it.Action = Update
		}
		plan.Items = append(plan.Items, it)
	}

	for i := range in.Existing {
		e := &in.Existing[i]
		if e.Managed && !used[e.ID] {
			plan.Items = append(plan.Items, Item{Action: Orphan, Key: e.Name, Name: e.Name, ExistingID: e.ID, Keep: e.Total})
		}
	}
	return plan
}

// diff preenche Add/Remove/Keep/Duplicates comparando desired com o que há na playlist.
func diff(it *Item, desired []string, have []Member) {
	want := make(map[string]bool, len(desired))
	for _, u := range desired {
		want[u] = true
	}
	count := map[string]int{}
	var removeSeen = map[string]bool{}
	for _, m := range have {
		count[m.URI]++
		if !want[m.URI] && !removeSeen[m.URI] {
			removeSeen[m.URI] = true
			it.Remove = append(it.Remove, m.URI)
		}
	}
	for _, u := range desired {
		if count[u] == 0 {
			it.Add = append(it.Add, u)
		} else {
			it.Keep++
		}
	}
	for u, n := range count {
		if want[u] && n > 1 {
			it.Duplicates += n - 1
		}
	}
}

// IgnoreRemovals descarta as remoções do plano. Usado quando há filtro ativo:
// faixas fora do filtro não são "indesejadas", apenas não foram consideradas.
// Playlists que só diferiam por remoções passam de "update" para "keep".
func (p *Plan) IgnoreRemovals() {
	for i := range p.Items {
		it := &p.Items[i]
		it.Remove = nil
		if it.Action == Update && len(it.Add) == 0 {
			it.Action = Keep
		}
	}
}
