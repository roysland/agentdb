package parse

import (
	"testing"
)

func TestGoParser_Embeds(t *testing.T) {
	code := []byte(`package testpkg

type InnerStruct struct {
	Value int
}

type OuterStruct struct {
	InnerStruct
	Name string
}

type BaseInterface interface {
	DoThing()
}

type DerivedInterface interface {
	BaseInterface
	DoAnotherThing()
}
`)

	p := &GoParser{}
	res, err := p.Parse("test.go", code)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	embedCount := 0
	for _, e := range res.Edges {
		if e.EdgeKind == "embeds" {
			embedCount++
		}
	}

	if embedCount != 2 {
		t.Errorf("Expected 2 embeds edges, got %d. Edges: %+v", embedCount, res.Edges)
	}
}
