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
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var errNextRoute = errors.New("gopress: next route")

type HandlerFunc func(*Request, *Response, NextFunc) error

type FastHandlerFunc func(*Request, *Response) error

type RawHandlerFunc func(http.ResponseWriter, *http.Request) error

type RawParamHandlerFunc func(http.ResponseWriter, *http.Request, Params) error

type RawSingleParamHandlerFunc func(http.ResponseWriter, *http.Request, string) error

type RawTwoParamHandlerFunc func(http.ResponseWriter, *http.Request, string, string) error

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
	layers                 []layer
	staticIndex            map[string][]int
	routeIndex             map[string][]int
	routeFallback          []int
	hasSlowLayer           bool
	hasOrderSensitiveLayer bool
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

	params routeParams
}

type Params struct {
	params routeParams
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
	method          string
	pattern         string
	compiled        routePattern
	prefix          string
	prefixSlash     string
	handlers        []HandlerFunc
	fastHandler     FastHandlerFunc
	rawHandler      RawHandlerFunc
	rawParamHandler RawParamHandlerFunc
	rawSingleParam  RawSingleParamHandlerFunc
	rawTwoParams    RawTwoParamHandlerFunc
	rawParamName    string
	rawParamName2   string
	rawParamDirect  bool
	fastOptions     FastRequestOptions
	errorHandlers   []ErrorHandlerFunc
}

type routePattern struct {
	static       bool
	staticPath   string
	segments     []routeSegment
	paramCount   int
	directSingle directSingleParamPattern
	directTwo    directTwoParamPattern
}

type routeSegment struct {
	kind  byte
	value string
}

type directSingleParamPattern struct {
	ok     bool
	prefix string
	suffix string
}

type directTwoParamPattern struct {
	ok     bool
	prefix string
	infix  string
}

type routeParam struct {
	name  string
	value string
}

type routeParams struct {
	inline [2]routeParam
	extra  []routeParam
	count  int
}

func (p *routeParams) append(name string, value string) {
	if p.count < len(p.inline) {
		p.inline[p.count] = routeParam{name: name, value: value}
		p.count++
		return
	}
	p.extra = append(p.extra, routeParam{name: name, value: value})
	p.count++
}

func (p routeParams) len() int {
	return p.count
}

func (p routeParams) at(idx int) routeParam {
	if idx < len(p.inline) {
		return p.inline[idx]
	}
	return p.extra[idx-len(p.inline)]
}

func (p routeParams) get(name string) string {
	for idx := 0; idx < p.len(); idx++ {
		param := p.at(idx)
		if param.name == name {
			return param.value
		}
	}
	return ""
}

func (p routeParams) directOrGet(name string, direct bool) string {
	if direct && p.len() == 1 {
		return p.at(0).value
	}
	return p.get(name)
}

func (p routeParams) twoDirectOrGet(first string, second string, direct bool) (string, string) {
	if direct && p.len() == 2 {
		return p.at(0).value, p.at(1).value
	}
	return p.get(first), p.get(second)
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

func (a *App) HandleRaw(method string, path string, handler RawHandlerFunc) *App {
	a.router.HandleRaw(method, path, handler)
	return a
}

func (a *App) HandleRawParams(method string, path string, handler RawParamHandlerFunc) *App {
	a.router.HandleRawParams(method, path, handler)
	return a
}

func (a *App) HandleRawParam(method string, path string, paramName string, handler RawSingleParamHandlerFunc) *App {
	a.router.HandleRawParam(method, path, paramName, handler)
	return a
}

func (a *App) HandleRawParams2(method string, path string, firstParam string, secondParam string, handler RawTwoParamHandlerFunc) *App {
	a.router.HandleRawParams2(method, path, firstParam, secondParam, handler)
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
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, handlers, nil, nil, nil, FastRequestOptions{}))
	return r
}

func (r *RouterGroup) HandleFast(method string, path string, handler FastHandlerFunc) *RouterGroup {
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, nil, handler, nil, nil, compatibleFastRequestOptions()))
	return r
}

func (r *RouterGroup) HandleFastOptions(method string, path string, options FastRequestOptions, handler FastHandlerFunc) *RouterGroup {
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, nil, handler, nil, nil, options))
	return r
}

func (r *RouterGroup) HandleRaw(method string, path string, handler RawHandlerFunc) *RouterGroup {
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, nil, nil, handler, nil, FastRequestOptions{}))
	return r
}

func (r *RouterGroup) HandleRawParams(method string, path string, handler RawParamHandlerFunc) *RouterGroup {
	r.addLayer(newRouteLayer(strings.ToUpper(method), path, nil, nil, nil, handler, FastRequestOptions{}))
	return r
}

func (r *RouterGroup) HandleRawParam(method string, path string, paramName string, handler RawSingleParamHandlerFunc) *RouterGroup {
	next := newRouteLayer(strings.ToUpper(method), path, nil, nil, nil, nil, FastRequestOptions{})
	next.rawSingleParam = handler
	next.rawParamName = paramName
	next.rawParamDirect = next.compiled.singleParamName() == paramName
	r.addLayer(next)
	return r
}

func (r *RouterGroup) HandleRawParams2(method string, path string, firstParam string, secondParam string, handler RawTwoParamHandlerFunc) *RouterGroup {
	next := newRouteLayer(strings.ToUpper(method), path, nil, nil, nil, nil, FastRequestOptions{})
	next.rawTwoParams = handler
	next.rawParamName = firstParam
	next.rawParamName2 = secondParam
	first, second, ok := next.compiled.twoParamNames()
	next.rawParamDirect = ok && first == firstParam && second == secondParam
	r.addLayer(next)
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
	if len(next.errorHandlers) > 0 {
		return
	}
	if next.pattern == "" {
		r.hasSlowLayer = true
		r.hasOrderSensitiveLayer = true
		return
	}
	if next.compiled.static {
		if r.staticIndex == nil {
			r.staticIndex = map[string][]int{}
		}
		r.staticIndex[next.compiled.staticPath] = append(r.staticIndex[next.compiled.staticPath], idx)
	}
	if segment, ok := next.compiled.firstIndexSegment(); ok {
		if r.routeIndex == nil {
			r.routeIndex = map[string][]int{}
		}
		r.routeIndex[segment] = append(r.routeIndex[segment], idx)
	} else {
		r.routeFallback = append(r.routeFallback, idx)
	}
	if next.compiled.static {
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
	if r.canServeRouteIndexOnly() {
		if r.serveRouteIndexOnly(w, native) {
			return
		}
		http.NotFound(w, native)
		return
	}
	if !r.hasSlowLayer {
		if r.serveStaticOnly(w, native) {
			return
		}
		http.NotFound(w, native)
		return
	}
	var req *Request
	var res *Response
	for idx := 0; idx < len(r.layers); idx++ {
		l := r.layers[idx]
		if len(l.errorHandlers) > 0 {
			continue
		}
		if l.rawSingleParam != nil && l.rawParamDirect {
			param, ok := l.matchesDirectParam(native.Method, native.URL.Path)
			if !ok {
				continue
			}
			if err := l.rawSingleParam(w, native, param); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		if l.rawTwoParams != nil && l.rawParamDirect {
			first, second, ok := l.matchesDirectTwoParams(native.Method, native.URL.Path)
			if !ok {
				continue
			}
			if err := l.rawTwoParams(w, native, first, second); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		params, ok := l.matches(native.Method, native.URL.Path)
		if !ok {
			continue
		}
		if l.rawHandler != nil {
			if err := l.rawHandler(w, native); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		if l.rawParamHandler != nil {
			if err := l.rawParamHandler(w, native, Params{params: params}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		if l.rawSingleParam != nil {
			if err := l.rawSingleParam(w, native, params.directOrGet(l.rawParamName, l.rawParamDirect)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		if l.rawTwoParams != nil {
			first, second := params.twoDirectOrGet(l.rawParamName, l.rawParamName2, l.rawParamDirect)
			if err := l.rawTwoParams(w, native, first, second); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
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
		if res == nil {
			res = NewResponse(w)
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
		if res != nil && res.written {
			return
		}
	}
	if res == nil || !res.written {
		http.NotFound(w, native)
	}
}

func (r *RouterGroup) serveStaticOnly(w http.ResponseWriter, native *http.Request) bool {
	candidates := r.staticCandidates(native.URL.Path)
	if len(candidates) == 0 {
		return false
	}
	var req *Request
	var res *Response
	for _, idx := range candidates {
		l := r.layers[idx]
		if l.method != "ALL" && l.method != native.Method {
			continue
		}
		if l.rawHandler != nil {
			if err := l.rawHandler(w, native); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		if l.rawParamHandler != nil {
			if err := l.rawParamHandler(w, native, Params{params: routeParams{}}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		if l.rawSingleParam != nil {
			if err := l.rawSingleParam(w, native, ""); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		if l.rawTwoParams != nil {
			if err := l.rawTwoParams(w, native, "", ""); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
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
		if res == nil {
			res = NewResponse(w)
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

func (r *RouterGroup) canServeRouteIndexOnly() bool {
	return r.hasSlowLayer &&
		!r.hasOrderSensitiveLayer &&
		len(r.routeFallback) == 0 &&
		len(r.routeIndex) > 0
}

func (r *RouterGroup) serveRouteIndexOnly(w http.ResponseWriter, native *http.Request) bool {
	candidates := r.routeCandidates(native.URL.Path)
	if len(candidates) == 0 {
		return false
	}
	var req *Request
	var res *Response
	for _, idx := range candidates {
		l := r.layers[idx]
		if l.rawSingleParam != nil && l.rawParamDirect {
			param, ok := l.matchesDirectParam(native.Method, native.URL.Path)
			if !ok {
				continue
			}
			if err := l.rawSingleParam(w, native, param); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		if l.rawTwoParams != nil && l.rawParamDirect {
			first, second, ok := l.matchesDirectTwoParams(native.Method, native.URL.Path)
			if !ok {
				continue
			}
			if err := l.rawTwoParams(w, native, first, second); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		params, ok := l.matches(native.Method, native.URL.Path)
		if !ok {
			continue
		}
		if l.rawHandler != nil {
			if err := l.rawHandler(w, native); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		if l.rawParamHandler != nil {
			if err := l.rawParamHandler(w, native, Params{params: params}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		if l.rawSingleParam != nil {
			if err := l.rawSingleParam(w, native, params.directOrGet(l.rawParamName, l.rawParamDirect)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
		}
		if l.rawTwoParams != nil {
			first, second := params.twoDirectOrGet(l.rawParamName, l.rawParamName2, l.rawParamDirect)
			if err := l.rawTwoParams(w, native, first, second); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return true
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
		if res == nil {
			res = NewResponse(w)
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
			return true
		}
		if res != nil && res.written {
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

func (r *RouterGroup) routeCandidates(path string) []int {
	if len(r.routeIndex) == 0 {
		return nil
	}
	path = trimLeadingSlashes(path)
	if path == "" {
		return nil
	}
	var out []int
	copied := false
	pos := 0
	for pos < len(path) {
		start := pos
		for pos < len(path) && path[pos] != '/' {
			pos++
		}
		if pos > start {
			out, copied = appendIndexedCandidates(out, copied, r.routeIndex[path[:pos]])
		}
		for pos < len(path) && path[pos] == '/' {
			pos++
		}
	}
	if copied && len(out) > 1 {
		sort.Ints(out)
	}
	return out
}

func appendIndexedCandidates(out []int, copied bool, candidates []int) ([]int, bool) {
	if len(candidates) == 0 {
		return out, copied
	}
	if out == nil {
		return candidates, false
	}
	if !copied {
		next := make([]int, 0, len(out)+len(candidates))
		next = append(next, out...)
		out = next
		copied = true
	}
	out = append(out, candidates...)
	return out, copied
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

func (l layer) matches(method string, path string) (routeParams, bool) {
	if l.pattern != "" {
		if l.method != "ALL" && l.method != method {
			return routeParams{}, false
		}
		return l.compiled.match(path)
	}
	if l.prefix != "" {
		return routeParams{}, pathHasPrefixPrepared(path, l.prefix, l.prefixSlash)
	}
	return routeParams{}, true
}

func (l layer) matchesDirectParam(method string, path string) (string, bool) {
	if l.pattern == "" || (l.method != "ALL" && l.method != method) {
		return "", false
	}
	return l.compiled.matchDirectParam(path)
}

func (l layer) matchesDirectTwoParams(method string, path string) (string, string, bool) {
	if l.pattern == "" || (l.method != "ALL" && l.method != method) {
		return "", "", false
	}
	return l.compiled.matchDirectTwoParams(path)
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

func QueryValue(req *http.Request, key string) string {
	if req == nil || req.URL == nil || key == "" {
		return ""
	}
	query := req.URL.RawQuery
	for len(query) > 0 {
		part := query
		if idx := strings.IndexByte(query, '&'); idx >= 0 {
			part = query[:idx]
			query = query[idx+1:]
		} else {
			query = ""
		}
		if part == "" {
			continue
		}
		rawKey := part
		rawValue := ""
		if idx := strings.IndexByte(part, '='); idx >= 0 {
			rawKey = part[:idx]
			rawValue = part[idx+1:]
		}
		if !queryComponentEqual(rawKey, key) {
			continue
		}
		return decodeQueryComponent(rawValue)
	}
	return ""
}

func HeaderValue(req *http.Request, key string) string {
	if req == nil {
		return ""
	}
	return req.Header.Get(key)
}

func CookieValue(req *http.Request, key string) string {
	if req == nil || key == "" {
		return ""
	}
	for _, header := range req.Header.Values("Cookie") {
		for len(header) > 0 {
			part := header
			if idx := strings.IndexByte(header, ';'); idx >= 0 {
				part = header[:idx]
				header = header[idx+1:]
			} else {
				header = ""
			}
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, value, ok := strings.Cut(part, "=")
			if !ok || strings.TrimSpace(name) != key {
				continue
			}
			return strings.Trim(value, `"`)
		}
	}
	return ""
}

func queryComponentEqual(raw string, value string) bool {
	if !strings.ContainsAny(raw, "+%") {
		return raw == value
	}
	decoded, err := url.QueryUnescape(raw)
	return err == nil && decoded == value
}

func decodeQueryComponent(raw string) string {
	if !strings.ContainsAny(raw, "+%") {
		return raw
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func (r *Request) Param(name string) string {
	if r.Params != nil {
		if value, ok := r.Params[name]; ok {
			return value
		}
	}
	return r.params.get(name)
}

func (p Params) Get(name string) string {
	return p.params.get(name)
}

func (r *Request) setRouteParams(params routeParams, materializeMap bool) {
	r.params = params
	if materializeMap {
		if params.len() == 0 {
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

func paramsToMap(params routeParams) map[string]string {
	values := make(map[string]string, params.len())
	for idx := 0; idx < params.len(); idx++ {
		param := params.at(idx)
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

func WriteRawString(w http.ResponseWriter, status int, contentType string, body string) error {
	if status == 0 {
		status = http.StatusOK
	}
	if contentType == "" {
		contentType = "text/plain"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, err := io.WriteString(w, body)
	return err
}

func WriteRawBytes(w http.ResponseWriter, status int, contentType string, body []byte) error {
	if status == 0 {
		status = http.StatusOK
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, err := w.Write(body)
	return err
}

func WriteJSONString(w http.ResponseWriter, status int, body string) error {
	return WriteRawString(w, status, "application/json", body)
}

func WriteJSONBytes(w http.ResponseWriter, status int, body []byte) error {
	return WriteRawBytes(w, status, "application/json", body)
}

func WriteJSON(w http.ResponseWriter, status int, value any) error {
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
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
	if !ok || params.len() == 0 {
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

func newRouteLayer(method string, path string, handlers []HandlerFunc, fastHandler FastHandlerFunc, rawHandler RawHandlerFunc, rawParamHandler RawParamHandlerFunc, fastOptions FastRequestOptions) layer {
	pattern := normalizePath(path)
	return layer{method: method, pattern: pattern, compiled: compileRoutePattern(pattern), handlers: handlers, fastHandler: fastHandler, rawHandler: rawHandler, rawParamHandler: rawParamHandler, fastOptions: fastOptions}
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
	compiled.directSingle = compileDirectSingleParamPattern(compiled)
	compiled.directTwo = compileDirectTwoParamPattern(compiled)
	return compiled
}

func (p routePattern) match(path string) (routeParams, bool) {
	if p.static {
		return routeParams{}, p.staticPath == path || normalizePath(path) == p.staticPath
	}

	pos := 0
	params := routeParams{}
	for i, segment := range p.segments {
		if segment.kind == '*' {
			params.append(segment.value, restPath(path, pos))
			return params, true
		}
		part, next, ok := nextPathSegment(path, pos)
		if !ok {
			return routeParams{}, false
		}
		switch segment.kind {
		case ':':
			params.append(segment.value, part)
		default:
			if segment.value != part {
				return routeParams{}, false
			}
		}
		pos = next
		if i == len(p.segments)-1 && hasMorePath(path, pos) {
			return routeParams{}, false
		}
	}
	return params, !hasMorePath(path, pos)
}

func (p routePattern) matchDirectParam(path string) (string, bool) {
	if p.directSingle.ok {
		return p.directSingle.match(path)
	}
	values, ok := p.matchDirectParams(path, 1)
	return values[0], ok
}

func (p routePattern) matchDirectTwoParams(path string) (string, string, bool) {
	if p.directTwo.ok {
		if first, second, ok := p.directTwo.match(path); ok {
			return first, second, true
		}
	}
	values, ok := p.matchDirectParams(path, 2)
	return values[0], values[1], ok
}

func (p routePattern) matchDirectParams(path string, want int) ([2]string, bool) {
	var values [2]string
	if p.static || p.paramCount != want {
		return values, false
	}
	pos := 0
	count := 0
	for i, segment := range p.segments {
		if segment.kind == '*' {
			if count >= want {
				return values, false
			}
			values[count] = restPath(path, pos)
			count++
			return values, count == want
		}
		part, next, ok := nextPathSegment(path, pos)
		if !ok {
			return values, false
		}
		switch segment.kind {
		case ':':
			if count >= want {
				return values, false
			}
			values[count] = part
			count++
		default:
			if segment.value != part {
				return values, false
			}
		}
		pos = next
		if i == len(p.segments)-1 && hasMorePath(path, pos) {
			return values, false
		}
	}
	return values, count == want && !hasMorePath(path, pos)
}

func (p routePattern) singleParamName() string {
	if p.paramCount != 1 {
		return ""
	}
	for _, segment := range p.segments {
		if segment.kind == ':' || segment.kind == '*' {
			return segment.value
		}
	}
	return ""
}

func (p routePattern) twoParamNames() (string, string, bool) {
	if p.paramCount != 2 {
		return "", "", false
	}
	var names [2]string
	idx := 0
	for _, segment := range p.segments {
		if segment.kind != ':' && segment.kind != '*' {
			continue
		}
		names[idx] = segment.value
		idx++
		if idx == len(names) {
			return names[0], names[1], true
		}
	}
	return "", "", false
}

func (p routePattern) firstIndexSegment() (string, bool) {
	if p.static {
		return staticPathIndexKey(p.staticPath)
	}
	if len(p.segments) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, segment := range p.segments {
		if segment.kind != 's' {
			break
		}
		if segment.value == "" {
			return "", false
		}
		if b.Len() > 0 {
			b.WriteByte('/')
		}
		b.WriteString(segment.value)
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

func compileDirectSingleParamPattern(pattern routePattern) directSingleParamPattern {
	if pattern.static || pattern.paramCount != 1 {
		return directSingleParamPattern{}
	}
	prefix := "/"
	suffix := ""
	seenParam := false
	for _, segment := range pattern.segments {
		switch segment.kind {
		case ':':
			if seenParam {
				return directSingleParamPattern{}
			}
			seenParam = true
		case '*':
			return directSingleParamPattern{}
		default:
			if !seenParam {
				if prefix != "/" {
					prefix += "/"
				}
				prefix += segment.value
				continue
			}
			suffix += "/" + segment.value
		}
	}
	if !seenParam {
		return directSingleParamPattern{}
	}
	if prefix != "/" {
		prefix += "/"
	}
	return directSingleParamPattern{ok: true, prefix: prefix, suffix: suffix}
}

func (p directSingleParamPattern) match(path string) (string, bool) {
	prefixLen := len(p.prefix)
	if len(path) < prefixLen || path[:prefixLen] != p.prefix {
		return "", false
	}
	start := prefixLen
	end := len(path)
	if p.suffix != "" {
		for end > prefixLen && path[end-1] == '/' {
			end--
		}
		suffixLen := len(p.suffix)
		if end < prefixLen+suffixLen || path[end-suffixLen:end] != p.suffix {
			return "", false
		}
		end -= suffixLen
		for end > start && path[end-1] == '/' {
			end--
		}
	} else {
		if start < end && path[start] != '/' && path[end-1] != '/' {
			if strings.IndexByte(path[start:end], '/') >= 0 {
				return "", false
			}
			return path[start:end], true
		}
		for end > prefixLen && path[end-1] == '/' {
			end--
		}
	}
	for start < end && path[start] == '/' {
		start++
	}
	if end <= start {
		return "", false
	}
	value := path[start:end]
	if strings.Contains(value, "/") {
		return "", false
	}
	return value, true
}

func compileDirectTwoParamPattern(pattern routePattern) directTwoParamPattern {
	if pattern.static || pattern.paramCount != 2 {
		return directTwoParamPattern{}
	}
	prefixParts := make([]string, 0, len(pattern.segments))
	infixParts := make([]string, 0, len(pattern.segments))
	paramCount := 0
	for _, segment := range pattern.segments {
		switch segment.kind {
		case ':':
			paramCount++
			if paramCount > 2 {
				return directTwoParamPattern{}
			}
		case '*':
			return directTwoParamPattern{}
		default:
			switch paramCount {
			case 0:
				prefixParts = append(prefixParts, segment.value)
			case 1:
				infixParts = append(infixParts, segment.value)
			default:
				return directTwoParamPattern{}
			}
		}
	}
	if paramCount != 2 {
		return directTwoParamPattern{}
	}
	return directTwoParamPattern{
		ok:     true,
		prefix: slashDelimitedPrefix(prefixParts),
		infix:  slashDelimitedPrefix(infixParts),
	}
}

func (p directTwoParamPattern) match(path string) (string, string, bool) {
	start, ok := consumeDirectPrefix(path, p.prefix)
	if !ok {
		return "", "", false
	}
	for start < len(path) && path[start] == '/' {
		start++
	}
	if start >= len(path) {
		return "", "", false
	}
	idx := strings.Index(path[start:], p.infix)
	if idx < 0 {
		return "", "", false
	}
	firstEnd := start + idx
	for firstEnd > start && path[firstEnd-1] == '/' {
		firstEnd--
	}
	if firstEnd <= start {
		return "", "", false
	}
	first := path[start:firstEnd]
	if strings.Contains(first, "/") {
		return "", "", false
	}
	secondStart := start + idx + len(p.infix)
	for secondStart < len(path) && path[secondStart] == '/' {
		secondStart++
	}
	secondEnd := len(path)
	for secondEnd > secondStart && path[secondEnd-1] == '/' {
		secondEnd--
	}
	if secondEnd <= secondStart {
		return "", "", false
	}
	second := path[secondStart:secondEnd]
	if strings.Contains(second, "/") {
		return "", "", false
	}
	return first, second, true
}

func slashDelimitedPrefix(parts []string) string {
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/") + "/"
}

func consumeDirectPrefix(path string, prefix string) (int, bool) {
	if prefix == "/" {
		pos := 0
		for pos < len(path) && path[pos] == '/' {
			pos++
		}
		return pos, true
	}
	if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
		return len(prefix), true
	}
	return 0, false
}

func staticPathIndexKey(path string) (string, bool) {
	path = trimLeadingSlashes(path)
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "", false
	}
	return path, true
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

func firstPathSegment(path string) (string, bool) {
	path = trimLeadingSlashes(path)
	if path == "" {
		return "", false
	}
	if idx := strings.IndexByte(path, '/'); idx >= 0 {
		return path[:idx], idx > 0
	}
	return path, true
}

func trimLeadingSlashes(path string) string {
	for len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	return path
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
