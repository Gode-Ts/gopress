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
	if !pattern.directSingle.ok {
		t.Fatal("expected single param route to precompute direct matcher")
	}
	value, ok := pattern.matchDirectParam("/users/123")
	if !ok || value != "123" {
		t.Fatalf("expected direct param 123, got value=%q ok=%v", value, ok)
	}
	if value, ok := compileRoutePattern("/users/:id/notes").matchDirectParam("/users/123/notes/"); !ok || value != "123" {
		t.Fatalf("expected middle direct param 123, got value=%q ok=%v", value, ok)
	}
	if value, ok := compileRoutePattern("/users/:id/notes").matchDirectParam("/users//123//notes"); !ok || value != "123" {
		t.Fatalf("expected duplicate slash direct param 123, got value=%q ok=%v", value, ok)
	}
	if _, ok := pattern.matchDirectParam("/users/123/extra"); ok {
		t.Fatal("expected trailing segment to miss")
	}
	if _, ok := pattern.matchDirectParam("/users/"); ok {
		t.Fatal("expected empty param to miss")
	}
}

func TestRoutePatternMatchDirectTwoParams(t *testing.T) {
	pattern := compileRoutePattern("/users/:userId/notes/:noteId")
	if !pattern.directTwo.ok {
		t.Fatal("expected two-param route to precompute direct matcher")
	}
	first, second, ok := pattern.matchDirectTwoParams("/users/u1/notes/n2")
	if !ok || first != "u1" || second != "n2" {
		t.Fatalf("expected direct params u1/n2, got first=%q second=%q ok=%v", first, second, ok)
	}
	if first, second, ok := pattern.matchDirectTwoParams("/users//u1//notes//n2/"); !ok || first != "u1" || second != "n2" {
		t.Fatalf("expected duplicate slash direct params u1/n2, got first=%q second=%q ok=%v", first, second, ok)
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

	if got := len(router.routeIndex["users"]); got != 1 {
		t.Fatalf("expected one users candidate, got %d", got)
	}
	if got := len(router.routeIndex["posts"]); got != 1 {
		t.Fatalf("expected one posts candidate, got %d", got)
	}
	if router.hasOrderSensitiveLayer {
		t.Fatal("plain dynamic routes should keep indexed dispatch enabled")
	}
}

func TestRouterIndexesMixedStaticAndDynamicRoutes(t *testing.T) {
	router := NewRouter()
	router.HandleRaw("GET", "/health", func(w http.ResponseWriter, _ *http.Request) error {
		return WriteRawString(w, http.StatusOK, "text/plain", "ok")
	})
	router.HandleRawParam("GET", "/users/:id", "id", func(w http.ResponseWriter, _ *http.Request, id string) error {
		return WriteRawString(w, http.StatusOK, "text/plain", id)
	})

	if !router.canServeRouteIndexOnly() {
		t.Fatal("mixed static and dynamic route-only apps should use indexed dispatch")
	}
	if got := len(router.routeIndex["health"]); got != 1 {
		t.Fatalf("expected one health candidate, got %d", got)
	}
	if got := len(router.routeIndex["users"]); got != 1 {
		t.Fatalf("expected one users candidate, got %d", got)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/123", nil))
	if rec.Body.String() != "123" {
		t.Fatalf("expected dynamic response 123, got %q", rec.Body.String())
	}
}

func TestRouterIndexesRoutesByLeadingStaticPrefix(t *testing.T) {
	router := NewRouter()
	router.HandleRawParam("GET", "/api/users/:id", "id", func(http.ResponseWriter, *http.Request, string) error {
		return nil
	})
	router.HandleRawParam("GET", "/api/posts/:id", "id", func(http.ResponseWriter, *http.Request, string) error {
		return nil
	})

	if got := len(router.routeIndex["api/users"]); got != 1 {
		t.Fatalf("expected one api/users candidate, got %d", got)
	}
	if got := len(router.routeIndex["api/posts"]); got != 1 {
		t.Fatalf("expected one api/posts candidate, got %d", got)
	}
	if got := len(router.routeIndex["api"]); got != 0 {
		t.Fatalf("expected no broad api candidates, got %d", got)
	}
}

func TestRouterLeadingStaticPrefixIndexIncludesParentCandidates(t *testing.T) {
	router := NewRouter()
	router.HandleRawParam("GET", "/api/:kind/:id", "kind", func(w http.ResponseWriter, _ *http.Request, kind string) error {
		return WriteRawString(w, http.StatusOK, "text/plain", "generic:"+kind)
	})
	router.HandleRawParam("GET", "/api/users/:id", "id", func(w http.ResponseWriter, _ *http.Request, id string) error {
		return WriteRawString(w, http.StatusOK, "text/plain", "users:"+id)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/123", nil))
	if rec.Body.String() != "generic:users" {
		t.Fatalf("expected parent candidate to preserve registration order, got %q", rec.Body.String())
	}
}

func TestRouterFirstSegmentIndexPreservesRegistrationOrder(t *testing.T) {
	router := NewRouter()
	router.HandleRawParam("GET", "/users/:id", "id", func(w http.ResponseWriter, _ *http.Request, id string) error {
		return WriteRawString(w, http.StatusOK, "text/plain", "dynamic:"+id)
	})
	router.HandleRaw("GET", "/users/new", func(w http.ResponseWriter, _ *http.Request) error {
		return WriteRawString(w, http.StatusOK, "text/plain", "static")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/new", nil))
	if rec.Body.String() != "dynamic:new" {
		t.Fatalf("expected dynamic route to preserve registration order, got %q", rec.Body.String())
	}
}

func BenchmarkRouterManyDynamicRawParamRoutesIndexed(b *testing.B) {
	benchmarkManyDynamicRawParamRoutes(b, true)
}

func BenchmarkRouterManyDynamicRawParamRoutesScan(b *testing.B) {
	benchmarkManyDynamicRawParamRoutes(b, false)
}

func BenchmarkRouterMixedStaticDynamicRoutesIndexed(b *testing.B) {
	benchmarkMixedStaticDynamicRoutes(b, true)
}

func BenchmarkRouterMixedStaticDynamicRoutesScan(b *testing.B) {
	benchmarkMixedStaticDynamicRoutes(b, false)
}

func BenchmarkRouterSharedPrefixDynamicRoutesIndexed(b *testing.B) {
	benchmarkSharedPrefixDynamicRoutes(b, true)
}

func BenchmarkRouterSharedPrefixDynamicRoutesScan(b *testing.B) {
	benchmarkSharedPrefixDynamicRoutes(b, false)
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

func benchmarkSharedPrefixDynamicRoutes(b *testing.B, indexed bool) {
	router := NewRouter()
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/api/resource-%d/:id", i)
		router.HandleRawParam(http.MethodGet, path, "id", func(w http.ResponseWriter, req *http.Request, id string) error {
			return WriteJSONBytes(w, http.StatusOK, []byte(`{"id":"`+id+`"}`))
		})
	}
	if !indexed {
		router.hasOrderSensitiveLayer = true
	}
	req := httptest.NewRequest(http.MethodGet, "/api/resource-99/123", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func benchmarkMixedStaticDynamicRoutes(b *testing.B, indexed bool) {
	router := NewRouter()
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("/health-%d", i)
		router.HandleRaw(http.MethodGet, path, func(w http.ResponseWriter, req *http.Request) error {
			return WriteRawString(w, http.StatusOK, "text/plain", "ok")
		})
	}
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
