package gopress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var errNextRoute = errors.New("gopress: next route")

type HandlerFunc func(*Request, *Response, NextFunc) error

type ErrorHandlerFunc func(error, *Request, *Response, NextFunc) error

type NextFunc func(args ...string) error

type App struct {
	router RouterGroup
}

type RouterGroup struct {
	layers []layer
}

type Route struct {
	Method   string
	Pattern  string
	Handlers []HandlerFunc
}

type Request struct {
	Native  *http.Request
	Params  map[string]string
	Query   map[string]string
	Headers map[string]string
	Cookies map[string]string
	Body    map[string]any
	Locals  map[string]any
	Method  string
	Path    string
}

type Response struct {
	writer      http.ResponseWriter
	status      int
	contentType string
	written     bool
}

type ServerConfig struct {
	Host            string
	Port            int
	ReadinessPath   string
	ShutdownTimeout time.Duration
	App             *App
}

type layer struct {
	method        string
	pattern       string
	prefix        string
	handlers      []HandlerFunc
	errorHandlers []ErrorHandlerFunc
}

func New() *App {
	return &App{}
}

func NewRouter() *RouterGroup {
	return &RouterGroup{}
}

func Router() *RouterGroup {
	return NewRouter()
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

func (a *App) Use(args ...any) *App {
	a.router.Use(args...)
	return a
}

func (a *App) UseError(handler ErrorHandlerFunc) *App {
	a.router.UseError(handler)
	return a
}

func (a *App) All(path string, handlers ...HandlerFunc) *App {
	a.router.All(path, handlers...)
	return a
}

func (a *App) Get(path string, handlers ...HandlerFunc) *App {
	a.router.Get(path, handlers...)
	return a
}

func (a *App) Post(path string, handlers ...HandlerFunc) *App {
	a.router.Post(path, handlers...)
	return a
}

func (a *App) Put(path string, handlers ...HandlerFunc) *App {
	a.router.Put(path, handlers...)
	return a
}

func (a *App) Patch(path string, handlers ...HandlerFunc) *App {
	a.router.Patch(path, handlers...)
	return a
}

func (a *App) Delete(path string, handlers ...HandlerFunc) *App {
	a.router.Delete(path, handlers...)
	return a
}

func (a *App) Options(path string, handlers ...HandlerFunc) *App {
	a.router.Options(path, handlers...)
	return a
}

func (a *App) Head(path string, handlers ...HandlerFunc) *App {
	a.router.Head(path, handlers...)
	return a
}

func (a *App) Route(path string) *RouteBuilder {
	return a.router.Route(path)
}

func (r *RouterGroup) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.serve(w, req, "")
}

func (r *RouterGroup) Use(args ...any) *RouterGroup {
	prefix := "/"
	start := 0
	if len(args) > 0 {
		if value, ok := args[0].(string); ok {
			prefix = normalizePath(value)
			start = 1
		}
	}
	for _, arg := range args[start:] {
		switch value := arg.(type) {
		case HandlerFunc:
			r.layers = append(r.layers, layer{prefix: prefix, handlers: []HandlerFunc{value}})
		case func(*Request, *Response, NextFunc) error:
			r.layers = append(r.layers, layer{prefix: prefix, handlers: []HandlerFunc{HandlerFunc(value)}})
		case *RouterGroup:
			r.mount(prefix, value)
		case *App:
			r.mount(prefix, &value.router)
		default:
			panic(fmt.Sprintf("gopress: unsupported middleware %T", arg))
		}
	}
	return r
}

func (r *RouterGroup) UseError(handler ErrorHandlerFunc) *RouterGroup {
	r.layers = append(r.layers, layer{prefix: "/", errorHandlers: []ErrorHandlerFunc{handler}})
	return r
}

func (r *RouterGroup) All(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle("ALL", path, handlers...)
}

func (r *RouterGroup) Get(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle(http.MethodGet, path, handlers...)
}

func (r *RouterGroup) Post(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle(http.MethodPost, path, handlers...)
}

func (r *RouterGroup) Put(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle(http.MethodPut, path, handlers...)
}

func (r *RouterGroup) Patch(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle(http.MethodPatch, path, handlers...)
}

func (r *RouterGroup) Delete(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle(http.MethodDelete, path, handlers...)
}

func (r *RouterGroup) Options(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle(http.MethodOptions, path, handlers...)
}

func (r *RouterGroup) Head(path string, handlers ...HandlerFunc) *RouterGroup {
	return r.Handle(http.MethodHead, path, handlers...)
}

func (r *RouterGroup) Handle(method string, path string, handlers ...HandlerFunc) *RouterGroup {
	r.layers = append(r.layers, layer{method: strings.ToUpper(method), pattern: normalizePath(path), handlers: handlers})
	return r
}

func (r *RouterGroup) Route(path string) *RouteBuilder {
	return &RouteBuilder{router: r, path: normalizePath(path)}
}

func (r *RouterGroup) mount(prefix string, child *RouterGroup) {
	for _, childLayer := range child.layers {
		next := childLayer
		if next.pattern != "" {
			next.pattern = joinPath(prefix, next.pattern)
		}
		if next.prefix != "" {
			next.prefix = joinPath(prefix, next.prefix)
		}
		r.layers = append(r.layers, next)
	}
}

type RouteBuilder struct {
	router *RouterGroup
	path   string
}

func (b *RouteBuilder) All(handlers ...HandlerFunc) *RouteBuilder {
	b.router.All(b.path, handlers...)
	return b
}

func (b *RouteBuilder) Get(handlers ...HandlerFunc) *RouteBuilder {
	b.router.Get(b.path, handlers...)
	return b
}

func (b *RouteBuilder) Post(handlers ...HandlerFunc) *RouteBuilder {
	b.router.Post(b.path, handlers...)
	return b
}

func (b *RouteBuilder) Put(handlers ...HandlerFunc) *RouteBuilder {
	b.router.Put(b.path, handlers...)
	return b
}

func (b *RouteBuilder) Patch(handlers ...HandlerFunc) *RouteBuilder {
	b.router.Patch(b.path, handlers...)
	return b
}

func (b *RouteBuilder) Delete(handlers ...HandlerFunc) *RouteBuilder {
	b.router.Delete(b.path, handlers...)
	return b
}

func (b *RouteBuilder) Options(handlers ...HandlerFunc) *RouteBuilder {
	b.router.Options(b.path, handlers...)
	return b
}

func (b *RouteBuilder) Head(handlers ...HandlerFunc) *RouteBuilder {
	b.router.Head(b.path, handlers...)
	return b
}

func (r *RouterGroup) serve(w http.ResponseWriter, native *http.Request, mountPrefix string) {
	req := NewRequest(native)
	res := NewResponse(w)
	for idx := 0; idx < len(r.layers); idx++ {
		l := r.layers[idx]
		if len(l.errorHandlers) > 0 {
			continue
		}
		params, ok := l.matches(native.Method, native.URL.Path)
		if !ok {
			continue
		}
		req.Params = params
		err, nextRoute := runHandlers(l.handlers, req, res)
		if nextRoute {
			continue
		}
		if err != nil {
			r.handleError(idx+1, err, req, res)
			return
		}
		if res.written {
			return
		}
	}
	if !res.written {
		http.NotFound(w, native)
	}
}

func runHandlers(handlers []HandlerFunc, req *Request, res *Response) (error, bool) {
	for _, handler := range handlers {
		nextCalled := false
		nextRoute := false
		nextErr := error(nil)
		next := func(args ...string) error {
			nextCalled = true
			if len(args) > 0 {
				if args[0] == "route" {
					nextRoute = true
					return errNextRoute
				}
				nextErr = errors.New(args[0])
				return nextErr
			}
			return nil
		}
		err := handler(req, res, next)
		if errors.Is(err, errNextRoute) || nextRoute {
			return nil, true
		}
		if err != nil {
			return err, false
		}
		if nextErr != nil {
			return nextErr, false
		}
		if !nextCalled {
			return nil, false
		}
	}
	return nil, false
}

func (r *RouterGroup) handleError(start int, err error, req *Request, res *Response) {
	for idx := start; idx < len(r.layers); idx++ {
		l := r.layers[idx]
		if len(l.errorHandlers) == 0 {
			continue
		}
		for _, handler := range l.errorHandlers {
			nextCalled := false
			nextErr := error(nil)
			next := func(args ...string) error {
				nextCalled = true
				if len(args) > 0 {
					nextErr = errors.New(args[0])
					return nextErr
				}
				return nil
			}
			if handlerErr := handler(err, req, res, next); handlerErr != nil {
				err = handlerErr
				continue
			}
			if res.written {
				return
			}
			if !nextCalled {
				return
			}
			if nextErr != nil {
				err = nextErr
			}
		}
	}
	if !res.written {
		http.Error(res.writer, err.Error(), http.StatusInternalServerError)
		res.written = true
	}
}

func (l layer) matches(method string, path string) (map[string]string, bool) {
	if l.pattern != "" {
		if l.method != "ALL" && l.method != method {
			return nil, false
		}
		return matchPattern(l.pattern, path)
	}
	if l.prefix != "" {
		return map[string]string{}, pathHasPrefix(path, l.prefix)
	}
	return map[string]string{}, true
}

func NewRequest(req *http.Request) *Request {
	query := map[string]string{}
	for key, values := range req.URL.Query() {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}
	headers := map[string]string{}
	for key, values := range req.Header {
		if len(values) > 0 {
			headers[strings.ToLower(key)] = values[0]
		}
	}
	cookies := map[string]string{}
	for _, cookie := range req.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	return &Request{
		Native:  req,
		Params:  map[string]string{},
		Query:   query,
		Headers: headers,
		Cookies: cookies,
		Body:    map[string]any{},
		Locals:  map[string]any{},
		Method:  req.Method,
		Path:    req.URL.Path,
	}
}

func NewResponse(w http.ResponseWriter) *Response {
	return &Response{writer: w, status: http.StatusOK}
}

func (r *Response) Status(status int) *Response {
	r.status = status
	return r
}

func (r *Response) Type(contentType string) *Response {
	if !strings.Contains(contentType, "/") {
		if resolved := mime.TypeByExtension("." + contentType); resolved != "" {
			contentType = resolved
		}
	}
	r.contentType = contentType
	return r
}

func (r *Response) Set(name string, value string) *Response {
	r.writer.Header().Set(name, value)
	return r
}

func (r *Response) Cookie(name string, value string) *Response {
	http.SetCookie(r.writer, &http.Cookie{Name: name, Value: value, Path: "/"})
	return r
}

func (r *Response) Send(body string) error {
	if r.contentType == "" {
		r.contentType = "text/plain"
	}
	r.writeHeaders()
	_, err := io.WriteString(r.writer, body)
	return err
}

func (r *Response) JSON(value any) error {
	if r.contentType == "" {
		r.contentType = "application/json"
	}
	r.writeHeaders()
	return json.NewEncoder(r.writer).Encode(value)
}

func (r *Response) Redirect(args ...any) error {
	status := http.StatusFound
	location := ""
	switch len(args) {
	case 0:
	case 1:
		location, _ = args[0].(string)
	default:
		if value, ok := args[0].(int); ok {
			status = value
		}
		location, _ = args[1].(string)
	}
	r.status = status
	r.Set("Location", location)
	return r.Send(http.StatusText(status))
}

func (r *Response) SendStatus(status int) error {
	r.status = status
	text := http.StatusText(status)
	if text == "" {
		text = strconv.Itoa(status)
	}
	return r.Send(text)
}

func (r *Response) SendFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if r.contentType == "" {
		if extType := mime.TypeByExtension(filepath.Ext(path)); extType != "" {
			r.contentType = extType
		} else {
			r.contentType = "application/octet-stream"
		}
	}
	r.writeHeaders()
	_, err = r.writer.Write(data)
	return err
}

func (r *Response) writeHeaders() {
	if r.written {
		return
	}
	if r.contentType != "" {
		r.writer.Header().Set("Content-Type", r.contentType)
	}
	r.writer.WriteHeader(r.status)
	r.written = true
}

func JSON() HandlerFunc {
	return func(req *Request, res *Response, next NextFunc) error {
		if !strings.Contains(req.Native.Header.Get("Content-Type"), "application/json") {
			return next()
		}
		data, err := io.ReadAll(req.Native.Body)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			return next()
		}
		return json.Unmarshal(data, &req.Body)
	}
}

func Static(root string) HandlerFunc {
	return func(req *Request, res *Response, next NextFunc) error {
		rel := strings.TrimPrefix(req.Path, "/")
		if idx := strings.LastIndex(rel, "/"); idx >= 0 {
			rel = rel[idx+1:]
		}
		path := filepath.Join(root, filepath.Clean(rel))
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return next()
		}
		return res.SendFile(path)
	}
}

func ListenAndServe(config ServerConfig) error {
	app := config.App
	if app == nil {
		app = New()
	}
	readinessPath := config.ReadinessPath
	if readinessPath == "" {
		readinessPath = "/__gode/ready"
	}
	addr := os.Getenv("GODE_WORKER_ADDR")
	if addr == "" {
		host := envOr("GODE_HOST", config.Host)
		if host == "" {
			host = "127.0.0.1"
		}
		port := envOr("GODE_PORT", strconv.Itoa(config.Port))
		if port == "" || port == "0" {
			port = "3000"
		}
		addr = host + ":" + port
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == readinessPath {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		app.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	if handshake := os.Getenv("GODE_WORKER_HANDSHAKE"); handshake != "" {
		_ = os.WriteFile(handshake, []byte(listener.Addr().String()), 0o644)
	}
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		shutdownTimeout := config.ShutdownTimeout
		if shutdownTimeout == 0 {
			shutdownTimeout = 10 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func matchPattern(pattern string, path string) (map[string]string, bool) {
	patternParts := splitPath(pattern)
	pathParts := splitPath(path)
	if len(patternParts) != len(pathParts) {
		return nil, false
	}
	params := map[string]string{}
	for i, part := range patternParts {
		if strings.HasPrefix(part, ":") {
			params[strings.TrimPrefix(part, ":")] = pathParts[i]
			continue
		}
		if strings.HasPrefix(part, "*") {
			params[strings.TrimPrefix(part, "*")] = strings.Join(pathParts[i:], "/")
			return params, true
		}
		if part != pathParts[i] {
			return nil, false
		}
	}
	return params, true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func pathHasPrefix(path string, prefix string) bool {
	prefix = normalizePath(prefix)
	if prefix == "/" {
		return true
	}
	path = normalizePath(path)
	return path == prefix || strings.HasPrefix(path, strings.TrimRight(prefix, "/")+"/")
}

func joinPath(prefix string, path string) string {
	if prefix == "/" {
		return normalizePath(path)
	}
	return normalizePath(strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(path, "/"))
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func envOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
