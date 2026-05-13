package gopress

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestRouterIndexesDynamicRoutesByFirstStaticSegment(t *testing.T) {
	router := NewRouter()
	router.HandleRawParam("GET", "/users/:id", "id", func(http.ResponseWriter, *http.Request, string) error {
		return nil
	})
	router.HandleRawParam("GET", "/posts/:id", "id", func(http.ResponseWriter, *http.Request, string) error {
		return nil
	})

	if got := len(router.dynamicIndex["users"]); got != 1 {
		t.Fatalf("expected one users candidate, got %d", got)
	}
	if got := len(router.dynamicIndex["posts"]); got != 1 {
		t.Fatalf("expected one posts candidate, got %d", got)
	}
	if router.hasOrderSensitiveLayer {
		t.Fatal("plain dynamic routes should keep indexed dispatch enabled")
	}
}

func BenchmarkRouterManyDynamicRawParamRoutesIndexed(b *testing.B) {
	benchmarkManyDynamicRawParamRoutes(b, true)
}

func BenchmarkRouterManyDynamicRawParamRoutesScan(b *testing.B) {
	benchmarkManyDynamicRawParamRoutes(b, false)
}

func benchmarkManyDynamicRawParamRoutes(b *testing.B, indexed bool) {
	router := NewRouter()
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/resource-%d/:id", i)
		router.HandleRawParam(http.MethodGet, path, "id", func(w http.ResponseWriter, req *http.Request, id string) error {
			return WriteJSONBytes(w, http.StatusOK, []byte(`{"id":"`+id+`"}`))
		})
	}
	if !indexed {
		router.hasOrderSensitiveLayer = true
	}
	req := httptest.NewRequest(http.MethodGet, "/resource-99/123", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}
