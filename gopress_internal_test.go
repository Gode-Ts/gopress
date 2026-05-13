package gopress

import "testing"

func TestRoutePatternMatchAvoidsHeapForSingleParam(t *testing.T) {
	pattern := compileRoutePattern("/users/:id")
	allocs := testing.AllocsPerRun(1000, func() {
		params, ok := pattern.match("/users/123")
		if !ok {
			t.Fatal("expected route match")
		}
		if params.len() != 1 || params.at(0).name != "id" || params.at(0).value != "123" {
			t.Fatalf("unexpected params: %#v", params)
		}
	})
	if allocs > 0 {
		t.Fatalf("expected single-param route match to avoid heap allocations, got %.2f", allocs)
	}
}

func TestRoutePatternMatchDirectParam(t *testing.T) {
	pattern := compileRoutePattern("/users/:id")
	value, ok := pattern.matchDirectParam("/users/123")
	if !ok || value != "123" {
		t.Fatalf("expected direct param 123, got value=%q ok=%v", value, ok)
	}
	if _, ok := pattern.matchDirectParam("/users/123/extra"); ok {
		t.Fatal("expected trailing segment to miss")
	}
}

func TestRoutePatternMatchDirectTwoParams(t *testing.T) {
	pattern := compileRoutePattern("/users/:userId/notes/:noteId")
	first, second, ok := pattern.matchDirectTwoParams("/users/u1/notes/n2")
	if !ok || first != "u1" || second != "n2" {
		t.Fatalf("expected direct params u1/n2, got first=%q second=%q ok=%v", first, second, ok)
	}
	if _, _, ok := pattern.matchDirectTwoParams("/users/u1/notes/n2/extra"); ok {
		t.Fatal("expected trailing segment to miss")
	}
}
