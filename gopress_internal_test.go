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
