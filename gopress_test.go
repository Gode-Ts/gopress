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

func TestFastHandlerAndJSONResponseFastPaths(t *testing.T) {
	app := gopress.New()
	app.HandleFast(http.MethodGet, "/json-string", func(req *gopress.Request, res *gopress.Response) error {
		return res.JSONString(`{"ok":true}`)
	})
	app.HandleFast(http.MethodGet, "/json-bytes", func(req *gopress.Request, res *gopress.Response) error {
		return res.JSONBytes([]byte(`{"ok":true}`))
	})
	app.HandleFast(http.MethodGet, "/status-json", func(req *gopress.Request, res *gopress.Response) error {
		return res.StatusJSON(http.StatusCreated, `{"created":true}`)
	})
	app.HandleFast(http.MethodGet, "/status-send", func(req *gopress.Request, res *gopress.Response) error {
		return res.StatusSend(http.StatusAccepted, "text/custom", "accepted")
	})

	for _, tc := range []struct {
		path        string
		status      int
		body        string
		contentType string
	}{
		{path: "/json-string", status: http.StatusOK, body: `{"ok":true}`, contentType: "application/json"},
		{path: "/json-bytes", status: http.StatusOK, body: `{"ok":true}`, contentType: "application/json"},
		{path: "/status-json", status: http.StatusCreated, body: `{"created":true}`, contentType: "application/json"},
		{path: "/status-send", status: http.StatusAccepted, body: "accepted", contentType: "text/custom"},
	} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

		if rec.Code != tc.status || rec.Body.String() != tc.body {
			t.Fatalf("%s unexpected response %d %q", tc.path, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("%s unexpected content type %q", tc.path, got)
		}
	}
}

func TestRawHandlerAndWriteHelpers(t *testing.T) {
	app := gopress.New()
	app.HandleRaw(http.MethodGet, "/json-bytes", func(w http.ResponseWriter, req *http.Request) error {
		return gopress.WriteJSONBytes(w, http.StatusCreated, []byte(`{"ok":true}`))
	})
	app.HandleRaw(http.MethodGet, "/json-string", func(w http.ResponseWriter, req *http.Request) error {
		return gopress.WriteJSONString(w, http.StatusAccepted, `{"ok":true}`)
	})
	app.HandleRaw(http.MethodGet, "/text", func(w http.ResponseWriter, req *http.Request) error {
		return gopress.WriteRawString(w, http.StatusTeapot, "text/custom", "short")
	})
	app.HandleRaw(http.MethodGet, "/json-value", func(w http.ResponseWriter, req *http.Request) error {
		return gopress.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	for _, tc := range []struct {
		path        string
		status      int
		body        string
		contentType string
	}{
		{path: "/json-bytes", status: http.StatusCreated, body: `{"ok":true}`, contentType: "application/json"},
		{path: "/json-string", status: http.StatusAccepted, body: `{"ok":true}`, contentType: "application/json"},
		{path: "/text", status: http.StatusTeapot, body: "short", contentType: "text/custom"},
		{path: "/json-value", status: http.StatusOK, body: "{\"ok\":true}\n", contentType: "application/json"},
	} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

		if rec.Code != tc.status || rec.Body.String() != tc.body {
			t.Fatalf("%s unexpected response %d %q", tc.path, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("%s unexpected content type %q", tc.path, got)
		}
	}
}

func TestRawParamHandlerReadsRouteParams(t *testing.T) {
	app := gopress.New()
	app.HandleRawParams(http.MethodGet, "/users/:id", func(w http.ResponseWriter, req *http.Request, params gopress.Params) error {
		return gopress.WriteJSONBytes(w, http.StatusOK, []byte(`{"id":"`+params.Get("id")+`"}`))
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/123", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != `{"id":"123"}` {
		t.Fatalf("unexpected response %d %q", rec.Code, rec.Body.String())
	}
}

func TestRawRequestValueHelpers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/search?page=42&name=Ada+Lovelace&empty=", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Cookie", "session=abc123; theme=dark")

	if got := gopress.QueryValue(req, "page"); got != "42" {
		t.Fatalf("unexpected query page %q", got)
	}
	if got := gopress.QueryValue(req, "name"); got != "Ada Lovelace" {
		t.Fatalf("unexpected decoded query name %q", got)
	}
	if got := gopress.QueryValue(req, "empty"); got != "" {
		t.Fatalf("unexpected empty query value %q", got)
	}
	if got := gopress.HeaderValue(req, "authorization"); got != "Bearer token" {
		t.Fatalf("unexpected header value %q", got)
	}
	if got := gopress.CookieValue(req, "session"); got != "abc123" {
		t.Fatalf("unexpected cookie value %q", got)
	}
	if got := gopress.CookieValue(req, "missing"); got != "" {
		t.Fatalf("unexpected missing cookie value %q", got)
	}
}

func TestFastHandlerOptionsAvoidUnneededMapsAndSupportParamLookup(t *testing.T) {
	app := gopress.New()
	app.HandleFastOptions(http.MethodGet, "/users/:id", gopress.FastRequestOptions{}, func(req *gopress.Request, res *gopress.Response) error {
		if req.Params != nil || req.Query != nil || req.Headers != nil || req.Cookies != nil || req.Body != nil || req.Locals != nil {
			t.Fatalf("fast request should avoid maps by default: %+v", req)
		}
		return res.Send(req.Param("id"))
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/123", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "123" {
		t.Fatalf("unexpected response %d %q", rec.Code, rec.Body.String())
	}
}

func TestFastHandlerFallthroughKeepsCompatibleRequestPath(t *testing.T) {
	app := gopress.New()
	app.HandleFastOptions(http.MethodGet, "/users/:id", gopress.FastRequestOptions{}, func(req *gopress.Request, res *gopress.Response) error {
		return nil
	})
	app.Get("/users/:id", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		if req.Params == nil || req.Query == nil || req.Headers == nil || req.Cookies == nil || req.Body == nil || req.Locals == nil {
			t.Fatalf("compatible request maps should be restored after fast fallthrough: %+v", req)
		}
		return res.Send(req.Params["id"])
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/123", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "123" {
		t.Fatalf("unexpected response %d %q", rec.Code, rec.Body.String())
	}
}

func TestStaticRouteIndexPreservesNextRouteOrder(t *testing.T) {
	app := gopress.New()
	app.Get("/health", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return next("route")
	})
	app.Get("/health", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.Send("fallback")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "fallback" {
		t.Fatalf("unexpected response %d %q", rec.Code, rec.Body.String())
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
