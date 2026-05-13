package gopress

import (
	"bytes"
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

type FastHandlerFunc func(*Request, *Response) error

type ErrorHandlerFunc func(error, *Request, *Response, NextFunc) error

type NextFunc func(args ...string) error

type FastRequestOptions struct {
	Params  bool
	Query   bool
	Headers bool
	Cookies bool
	Body    bool
	Locals  bool
}

type App struct {
	router RouterGroup
}

type RouterGroup struct {
	layers       []layer
	staticIndex  map[string][]int
	hasSlowLayer bool
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

	params []routeParam
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
	compiled      routePattern
	prefix        string
	prefixSlash   string
	handlers      []HandlerFunc
	fastHandler   FastHandlerFunc
	fastOptions   FastRequestOptions
	errorHandlers []ErrorHandlerFunc
}

type routePattern struct {
	static     bool
	staticPath string
	segments   []routeSegment
	paramCount int
}

type routeSegment struct {
	kind  byte
	value string
}

type routeParam struct {
	name  string
	value string
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

func (a *App) HandleFast(method string, path string, handler FastHandlerFunc) *App {
	a.router.HandleFast(method, path, handler)
	return a
}

func (a *App) HandleFastOptions(method string, path string, options FastRequestOptions, handler FastHandlerFunc) *App {
	a.router.HandleFastOptions(method, path, options, handler)
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
			r.addLayer(newMiddlewareLayer(prefix, []HandlerFunc{value}))
		case func(*Request, *Response, NextFunc) error:
			r.addLayer(newMiddlewareLayer(prefix, []HandlerFunc{HandlerFunc(value)}))
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
	r.addLayer(layer{prefix: "/", prefixSlash: "/", errorHandlers: []ErrorHandlerFunc{handler}})
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
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, handlers, nil, FastRequestOptions{}))
	return r
}

func (r *RouterGroup) HandleFast(method string, path string, handler FastHandlerFunc) *RouterGroup {
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, nil, handler, compatibleFastRequestOptions()))
	return r
}

func (r *RouterGroup) HandleFastOptions(method string, path string, options FastRequestOptions, handler FastHandlerFunc) *RouterGroup {
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, nil, handler, options))
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
			next.compiled = compileRoutePattern(next.pattern)
		}
		if next.prefix != "" {
			next.prefix = joinPath(prefix, next.prefix)
			next.prefixSlash = prefixSlash(next.prefix)
		}
		r.addLayer(next)
	}
}

func (r *RouterGroup) addLayer(next layer) {
	idx := len(r.layers)
	r.layers = append(r.layers, next)
	if next.pattern != "" && next.compiled.static {
		if r.staticIndex == nil {
			r.staticIndex = map[string][]int{}
		}
		r.staticIndex[next.compiled.staticPath] = append(r.staticIndex[next.compiled.staticPath], idx)
		return
	}
	r.hasSlowLayer = true
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
	if !r.hasSlowLayer {
		if r.serveStaticOnly(w, native) {
			return
		}
		http.NotFound(w, native)
		return
	}
	var req *Request
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
		if req == nil {
			if l.fastHandler != nil {
				req = NewFastRequestWithOptions(native, l.fastOptions)
			} else {
				req = NewRequest(native)
			}
		} else if l.fastHandler != nil {
			req.ensureOptions(native, l.fastOptions)
		} else {
			req.ensureOptions(native, compatibleFastRequestOptions())
		}
		req.setRouteParams(params, l.fastHandler == nil || l.fastOptions.Params)
		var err error
		nextRoute := false
		if l.fastHandler != nil {
			err = l.fastHandler(req, res)
		} else {
			err, nextRoute = runHandlers(l.handlers, req, res)
		}
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

func (r *RouterGroup) serveStaticOnly(w http.ResponseWriter, native *http.Request) bool {
	candidates := r.staticCandidates(native.URL.Path)
	if len(candidates) == 0 {
		return false
	}
	var req *Request
	res := NewResponse(w)
	for _, idx := range candidates {
		l := r.layers[idx]
		if l.method != "ALL" && l.method != native.Method {
			continue
		}
		if req == nil {
			if l.fastHandler != nil {
				req = NewFastRequestWithOptions(native, l.fastOptions)
			} else {
				req = NewRequest(native)
			}
		} else if l.fastHandler != nil {
			req.ensureOptions(native, l.fastOptions)
		} else {
			req.ensureOptions(native, compatibleFastRequestOptions())
		}
		var err error
		nextRoute := false
		if l.fastHandler != nil {
			err = l.fastHandler(req, res)
		} else {
			err, nextRoute = runHandlers(l.handlers, req, res)
		}
		if nextRoute {
			continue
		}
		if err != nil {
			r.handleError(idx+1, err, req, res)
			return true
		}
		if res.written {
			return true
		}
	}
	return false
}

func (r *RouterGroup) staticCandidates(path string) []int {
	if len(r.staticIndex) == 0 {
		return nil
	}
	if candidates := r.staticIndex[path]; len(candidates) > 0 {
		return candidates
	}
	normalized := normalizePath(path)
	if normalized == path {
		return nil
	}
	return r.staticIndex[normalized]
}

func runHandlers(handlers []HandlerFunc, req *Request, res *Response) (error, bool) {
	state := nextState{}
	next := func(args ...string) error {
		state.called = true
		if len(args) > 0 {
			if args[0] == "route" {
				state.route = true
				return errNextRoute
			}
			state.err = errors.New(args[0])
			return state.err
		}
		return nil
	}
	for _, handler := range handlers {
		state = nextState{}
		err := handler(req, res, next)
		if errors.Is(err, errNextRoute) || state.route {
			return nil, true
		}
		if err != nil {
			return err, false
		}
		if state.err != nil {
			return state.err, false
		}
		if !state.called {
			return nil, false
		}
	}
	return nil, false
}

type nextState struct {
	called bool
	route  bool
	err    error
}

func (r *RouterGroup) handleError(start int, err error, req *Request, res *Response) {
	for idx := start; idx < len(r.layers); idx++ {
		l := r.layers[idx]
		if len(l.errorHandlers) == 0 {
			continue
		}
		state := nextState{}
		next := func(args ...string) error {
			state.called = true
			if len(args) > 0 {
				state.err = errors.New(args[0])
				return state.err
			}
			return nil
		}
		for _, handler := range l.errorHandlers {
			state = nextState{}
			if handlerErr := handler(err, req, res, next); handlerErr != nil {
				err = handlerErr
				continue
			}
			if res.written {
				return
			}
			if !state.called {
				return
			}
			if state.err != nil {
				err = state.err
			}
		}
	}
	if !res.written {
		http.Error(res.writer, err.Error(), http.StatusInternalServerError)
		res.written = true
	}
}

func (l layer) matches(method string, path string) ([]routeParam, bool) {
	if l.pattern != "" {
		if l.method != "ALL" && l.method != method {
			return nil, false
		}
		return l.compiled.match(path)
	}
	if l.prefix != "" {
		return nil, pathHasPrefixPrepared(path, l.prefix, l.prefixSlash)
	}
	return nil, true
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

func NewFastRequest(req *http.Request) *Request {
	return NewFastRequestWithOptions(req, compatibleFastRequestOptions())
}

func NewFastRequestWithOptions(req *http.Request, options FastRequestOptions) *Request {
	request := &Request{
		Native: req,
		Method: req.Method,
		Path:   req.URL.Path,
	}
	if options.Params {
		request.Params = map[string]string{}
	}
	if options.Query {
		request.Query = firstQueryValues(req)
	}
	if options.Headers {
		request.Headers = firstHeaderValues(req)
	}
	if options.Cookies {
		request.Cookies = cookieValues(req)
	}
	if options.Body {
		request.Body = map[string]any{}
	}
	if options.Locals {
		request.Locals = map[string]any{}
	}
	return request
}

func compatibleFastRequestOptions() FastRequestOptions {
	return FastRequestOptions{
		Params:  true,
		Query:   true,
		Headers: true,
		Cookies: true,
		Body:    true,
		Locals:  true,
	}
}

func firstQueryValues(req *http.Request) map[string]string {
	query := map[string]string{}
	for key, values := range req.URL.Query() {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}
	return query
}

func firstHeaderValues(req *http.Request) map[string]string {
	headers := map[string]string{}
	for key, values := range req.Header {
		if len(values) > 0 {
			headers[strings.ToLower(key)] = values[0]
		}
	}
	return headers
}

func cookieValues(req *http.Request) map[string]string {
	cookies := map[string]string{}
	for _, cookie := range req.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	return cookies
}

func (r *Request) Param(name string) string {
	if r.Params != nil {
		if value, ok := r.Params[name]; ok {
			return value
		}
	}
	for _, param := range r.params {
		if param.name == name {
			return param.value
		}
	}
	return ""
}

func (r *Request) setRouteParams(params []routeParam, materializeMap bool) {
	r.params = params
	if materializeMap {
		if len(params) == 0 {
			if r.Params == nil {
				r.Params = map[string]string{}
			}
			return
		}
		r.Params = paramsToMap(params)
		return
	}
	r.Params = nil
}

func (r *Request) ensureOptions(req *http.Request, options FastRequestOptions) {
	if options.Params && r.Params == nil {
		r.Params = map[string]string{}
	}
	if options.Query && r.Query == nil {
		r.Query = firstQueryValues(req)
	}
	if options.Headers && r.Headers == nil {
		r.Headers = firstHeaderValues(req)
	}
	if options.Cookies && r.Cookies == nil {
		r.Cookies = cookieValues(req)
	}
	if options.Body && r.Body == nil {
		r.Body = map[string]any{}
	}
	if options.Locals && r.Locals == nil {
		r.Locals = map[string]any{}
	}
}

func paramsToMap(params []routeParam) map[string]string {
	values := make(map[string]string, len(params))
	for _, param := range params {
		values[param.name] = param.value
	}
	return values
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

func (r *Response) StatusSend(status int, contentType string, body string) error {
	r.status = status
	if contentType != "" {
		r.contentType = contentType
	}
	return r.Send(body)
}

func (r *Response) JSON(value any) error {
	if r.contentType == "" {
		r.contentType = "application/json"
	}
	r.writeHeaders()
	return json.NewEncoder(r.writer).Encode(value)
}

func (r *Response) JSONString(body string) error {
	if r.contentType == "" {
		r.contentType = "application/json"
	}
	r.writeHeaders()
	_, err := io.WriteString(r.writer, body)
	return err
}

func (r *Response) JSONBytes(body []byte) error {
	if r.contentType == "" {
		r.contentType = "application/json"
	}
	r.writeHeaders()
	_, err := r.writer.Write(body)
	return err
}

func (r *Response) StatusJSON(status int, body string) error {
	r.status = status
	return r.JSONString(body)
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
		if len(bytes.TrimSpace(data)) == 0 {
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
	params, ok := compileRoutePattern(normalizePath(pattern)).match(path)
	if !ok || len(params) == 0 {
		return nil, ok
	}
	return paramsToMap(params), true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func pathHasPrefix(path string, prefix string) bool {
	return pathHasPrefixPrepared(path, normalizePath(prefix), prefixSlash(prefix))
}

func pathHasPrefixPrepared(path string, prefix string, slash string) bool {
	if prefix == "/" {
		return true
	}
	path = normalizePath(path)
	return path == prefix || strings.HasPrefix(path, slash)
}

func newRouteLayer(method string, path string, handlers []HandlerFunc, fastHandler FastHandlerFunc, fastOptions FastRequestOptions) layer {
	pattern := normalizePath(path)
	return layer{method: method, pattern: pattern, compiled: compileRoutePattern(pattern), handlers: handlers, fastHandler: fastHandler, fastOptions: fastOptions}
}

func newMiddlewareLayer(prefix string, handlers []HandlerFunc) layer {
	prefix = normalizePath(prefix)
	return layer{prefix: prefix, prefixSlash: prefixSlash(prefix), handlers: handlers}
}

func prefixSlash(prefix string) string {
	prefix = normalizePath(prefix)
	if prefix == "/" {
		return "/"
	}
	return strings.TrimRight(prefix, "/") + "/"
}

func compileRoutePattern(pattern string) routePattern {
	parts := splitPath(pattern)
	compiled := routePattern{static: true, staticPath: pattern, segments: make([]routeSegment, 0, len(parts))}
	for _, part := range parts {
		switch {
		case strings.HasPrefix(part, ":"):
			compiled.static = false
			compiled.paramCount++
			compiled.segments = append(compiled.segments, routeSegment{kind: ':', value: strings.TrimPrefix(part, ":")})
		case strings.HasPrefix(part, "*"):
			compiled.static = false
			compiled.paramCount++
			compiled.segments = append(compiled.segments, routeSegment{kind: '*', value: strings.TrimPrefix(part, "*")})
		default:
			compiled.segments = append(compiled.segments, routeSegment{kind: 's', value: part})
		}
	}
	return compiled
}

func (p routePattern) match(path string) ([]routeParam, bool) {
	if p.static {
		return nil, p.staticPath == path || normalizePath(path) == p.staticPath
	}

	pos := 0
	var params []routeParam
	for i, segment := range p.segments {
		if segment.kind == '*' {
			if params == nil && p.paramCount > 0 {
				params = make([]routeParam, 0, p.paramCount)
			}
			params = append(params, routeParam{name: segment.value, value: restPath(path, pos)})
			return params, true
		}
		part, next, ok := nextPathSegment(path, pos)
		if !ok {
			return nil, false
		}
		switch segment.kind {
		case ':':
			if params == nil {
				params = make([]routeParam, 0, p.paramCount)
			}
			params = append(params, routeParam{name: segment.value, value: part})
		default:
			if segment.value != part {
				return nil, false
			}
		}
		pos = next
		if i == len(p.segments)-1 && hasMorePath(path, pos) {
			return nil, false
		}
	}
	return params, !hasMorePath(path, pos)
}

func nextPathSegment(path string, pos int) (string, int, bool) {
	for pos < len(path) && path[pos] == '/' {
		pos++
	}
	if pos >= len(path) {
		return "", pos, false
	}
	start := pos
	for pos < len(path) && path[pos] != '/' {
		pos++
	}
	return path[start:pos], pos, true
}

func restPath(path string, pos int) string {
	for pos < len(path) && path[pos] == '/' {
		pos++
	}
	return path[pos:]
}

func hasMorePath(path string, pos int) bool {
	for pos < len(path) && path[pos] == '/' {
		pos++
	}
	return pos < len(path)
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
