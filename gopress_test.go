package gopress_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gode-Ts/gopress"
)

func TestRouteParamsAndResponseHelpers(t *testing.T) {
	app := gopress.New()
	app.Get("/users/:id", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		if req.Method != http.MethodGet || req.Path != "/users/123" {
			t.Fatalf("unexpected request fields: %+v", req)
		}
		return res.Status(201).Type("text/plain").Send(req.Params["id"])
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/123", nil))

	if rec.Code != http.StatusCreated || rec.Body.String() != "123" {
		t.Fatalf("unexpected response %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("unexpected content type %q", got)
	}
}

func TestMiddlewareRouterMountAndJSONBody(t *testing.T) {
	app := gopress.New()
	app.Use(gopress.JSON())
	app.Use(func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		req.Locals["global"] = "yes"
		return next()
	})
	router := gopress.Router()
	router.Use(func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		req.Locals["router"] = "yes"
		return next()
	})
	router.Post("/users/:id", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.JSON(map[string]any{
			"id":     req.Params["id"],
			"name":   req.Body["name"],
			"global": req.Locals["global"],
			"router": req.Locals["router"],
		})
	})
	app.Use("/api", router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/users/42", stringsReader(`{"name":"Ada"}`))
	req.Header.Set("Content-Type", "application/json")
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body %q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"global":"yes","id":"42","name":"Ada","router":"yes"}`+"\n" {
		t.Fatalf("unexpected json body %q", rec.Body.String())
	}
}

func TestNextRouteSkipsToNextMatchingRoute(t *testing.T) {
	app := gopress.New()
	app.Get("/users/:id", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		if req.Params["id"] == "0" {
			return next("route")
		}
		return res.Send("normal")
	})
	app.Get("/users/:id", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.Send("fallback")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/0", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "fallback" {
		t.Fatalf("unexpected response %d %q", rec.Code, rec.Body.String())
	}
}

func TestErrorMiddlewareHandlesHandlerError(t *testing.T) {
	app := gopress.New()
	app.Get("/boom", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return errors.New("boom")
	})
	app.UseError(func(err error, req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.Status(503).Send("handled:" + err.Error())
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "handled:boom" {
		t.Fatalf("unexpected response %d %q", rec.Code, rec.Body.String())
	}
}

func TestRouteBuilderRedirectAndSendStatus(t *testing.T) {
	app := gopress.New()
	app.Route("/go").Get(func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.Redirect("/target")
	})
	app.Get("/status", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.SendStatus(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/go", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/target" {
		t.Fatalf("unexpected redirect response %d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected sendStatus response %d", rec.Code)
	}
}

func TestStaticAndSendFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := gopress.New()
	app.Use("/public", gopress.Static(dir))
	app.Get("/download", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.SendFile(filepath.Join(dir, "hello.txt"))
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public/hello.txt", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("unexpected static response %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/download", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("unexpected sendfile response %d %q", rec.Code, rec.Body.String())
	}
}

func stringsReader(value string) *strings.Reader { return strings.NewReader(value) }
