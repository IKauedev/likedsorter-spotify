package planner

import (
	"testing"

	"github.com/ikauedeveloper/likedsorter/internal/grouping"
)

func TestIgnoreRemovals(t *testing.T) {
	p := Build(Input{
		Groups: []grouping.Group{group("Rock", "a"), group("Jazz", "x", "y")},
		Existing: []Existing{
			{ID: "r", Name: "Curtidas • Rock", Managed: true, Loaded: true, Items: uris("a", "fora-do-filtro")}, // só diferia por remoção
			{ID: "j", Name: "Curtidas • Jazz", Managed: true, Loaded: true, Items: uris("x", "outra")},          // tem adição E remoção
		},
	})
	if find(p, "Curtidas • Rock").Action != Update || find(p, "Curtidas • Jazz").Action != Update {
		t.Fatal("pré-condição")
	}
	p.IgnoreRemovals()
	rock, jazz := find(p, "Curtidas • Rock"), find(p, "Curtidas • Jazz")
	if rock.Action != Keep || len(rock.Remove) != 0 {
		t.Errorf("só remoção → manter: %+v", rock)
	}
	if jazz.Action != Update || len(jazz.Remove) != 0 || len(jazz.Add) != 1 {
		t.Errorf("adição permanece: %+v", jazz)
	}
	if tot := p.Totals(); tot.Remove != 0 {
		t.Errorf("totais: %+v", tot)
	}
}
