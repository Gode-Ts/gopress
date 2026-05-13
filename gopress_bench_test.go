package gopress_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Gode-Ts/gopress"
)

func BenchmarkGopressStaticRouteFast(b *testing.B) {
	app := gopress.New()
	app.HandleFastOptions(http.MethodGet, "/bench", gopress.FastRequestOptions{}, func(req *gopress.Request, res *gopress.Response) error {
		return res.StatusJSON(http.StatusOK, `{"runtime":"gopress"}`)
	})
	req := httptest.NewRequest(http.MethodGet, "/bench", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressParamRoute(b *testing.B) {
	app := gopress.New()
	app.Get("/users/:id", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.Send(req.Params["id"])
	})
	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressParamRouteFast(b *testing.B) {
	app := gopress.New()
	app.HandleFastOptions(http.MethodGet, "/users/:id", gopress.FastRequestOptions{}, func(req *gopress.Request, res *gopress.Response) error {
		return res.Send(req.Param("id"))
	})
	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressMiddlewareChain(b *testing.B) {
	app := gopress.New()
	for i := 0; i < 10; i++ {
		app.Use(func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
			return next()
		})
	}
	app.Get("/middleware", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.Send("ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/middleware", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressNotFoundManyRoutes(b *testing.B) {
	app := gopress.New()
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/route-%d", i)
		app.Get(path, func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
			return res.Send("ok")
		})
	}
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressJSONResponse(b *testing.B) {
	app := gopress.New()
	app.Get("/json", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.JSON(map[string]any{"id": "123", "ok": true})
	})
	req := httptest.NewRequest(http.MethodGet, "/json", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressJSONBytesResponse(b *testing.B) {
	app := gopress.New()
	app.HandleFastOptions(http.MethodGet, "/json", gopress.FastRequestOptions{}, func(req *gopress.Request, res *gopress.Response) error {
		return res.JSONBytes([]byte(`{"id":"123","ok":true}`))
	})
	req := httptest.NewRequest(http.MethodGet, "/json", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressRawJSONBytesResponse(b *testing.B) {
	app := gopress.New()
	app.HandleRaw(http.MethodGet, "/json", func(w http.ResponseWriter, req *http.Request) error {
		return gopress.WriteJSONBytes(w, http.StatusOK, []byte(`{"id":"123","ok":true}`))
	})
	req := httptest.NewRequest(http.MethodGet, "/json", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressRawParamJSONBytesResponse(b *testing.B) {
	app := gopress.New()
	app.HandleRawParams(http.MethodGet, "/users/:id", func(w http.ResponseWriter, req *http.Request, params gopress.Params) error {
		return gopress.WriteJSONBytes(w, http.StatusOK, []byte(`{"id":"`+params.Get("id")+`"}`))
	})
	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressRawSingleParamJSONBytesResponse(b *testing.B) {
	app := gopress.New()
	app.HandleRawParam(http.MethodGet, "/users/:id", "id", func(w http.ResponseWriter, req *http.Request, id string) error {
		return gopress.WriteJSONBytes(w, http.StatusOK, []byte(`{"id":"`+id+`"}`))
	})
	req := httptest.NewRequest(http.MethodGet, "/users/123", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressRawTwoParamJSONBytesResponse(b *testing.B) {
	app := gopress.New()
	app.HandleRawParams2(http.MethodGet, "/users/:userId/notes/:noteId", "userId", "noteId", func(w http.ResponseWriter, req *http.Request, userId string, noteId string) error {
		return gopress.WriteJSONBytes(w, http.StatusOK, []byte(`{"userId":"`+userId+`","noteId":"`+noteId+`"}`))
	})
	req := httptest.NewRequest(http.MethodGet, "/users/u1/notes/n2", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressRawQueryJSONBytesResponse(b *testing.B) {
	app := gopress.New()
	app.HandleRaw(http.MethodGet, "/search", func(w http.ResponseWriter, req *http.Request) error {
		return gopress.WriteJSONBytes(w, http.StatusOK, []byte(`{"page":"`+gopress.QueryValue(req, "page")+`"}`))
	})
	req := httptest.NewRequest(http.MethodGet, "/search?page=42", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressRawJSONBytesWithErrorMiddleware(b *testing.B) {
	app := gopress.New()
	app.UseError(func(err error, req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.StatusSend(http.StatusInternalServerError, "text/plain", err.Error())
	})
	app.HandleRaw(http.MethodGet, "/json", func(w http.ResponseWriter, req *http.Request) error {
		return gopress.WriteJSONBytes(w, http.StatusOK, []byte(`{"id":"123","ok":true}`))
	})
	req := httptest.NewRequest(http.MethodGet, "/json", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkGopressJSONBody(b *testing.B) {
	app := gopress.New()
	app.Use(gopress.JSON())
	app.Post("/echo", func(req *gopress.Request, res *gopress.Response, next gopress.NextFunc) error {
		return res.JSON(req.Body)
	})
	body := []byte(`{"name":"Ada","role":"engineer"}`)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		app.ServeHTTP(rec, req)
	}
}

func BenchmarkNativeHTTPStaticRoute(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/bench" {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"runtime":"native"}`)
	})
	req := httptest.NewRequest(http.MethodGet, "/bench", nil)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
