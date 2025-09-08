package treesitter

import "testing"

func TestSymbolGraph_StoreRetrieve(t *testing.T) {
	g := NewSymbolGraph()
	s := &Symbol{ID: "A:1:1", Name: "A"}
	g.AddSymbol(s)
	got, ok := g.GetSymbol("A:1:1")
	if !ok || got.Name != "A" {
		t.Fatalf("symbol not found or wrong: %#v, %v", got, ok)
	}
}

func TestSymbolGraph_Relationships(t *testing.T) {
	g := NewSymbolGraph()
	rel := Relationship{From: "A:1:1", To: "B", Type: RelationshipTypeCall}
	g.AddRelationship(rel)
	rs := g.GetRelationships("A:1:1")
	if len(rs) != 1 || rs[0].Type != RelationshipTypeCall {
		t.Fatalf("relationship mismatch: %v", rs)
	}
}

func TestGenerateSymbolID_Format(t *testing.T) {
	if id := GenerateSymbolID("Foo", Position{Line: 10, Column: 2}); id != "Foo:10:2" {
		t.Fatalf("GenerateSymbolID mismatch: %s", id)
	}
}
