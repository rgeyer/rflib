// Package api is rflib's JSON HTTP API: the service layer (package app)
// over HTTP, described by api/openapi.yaml. Handlers decode requests into
// service calls and encode the results as the DTOs in dto.go; the rules
// stay in package app.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ryan/rflib/internal/app"
	"github.com/ryan/rflib/internal/domain"
	"github.com/ryan/rflib/internal/store"
)

// Prefix is where the API is served.
const Prefix = "/api/v1"

// maxBody limits request bodies, imports included.
const maxBody = 32 << 20

// Options configure the API.
type Options struct {
	// Token, when set, is required as "Authorization: Bearer TOKEN" on
	// every request but the health check and the spec.
	Token string
	// Logger records server errors; nil discards them.
	Logger *slog.Logger
	// Spec is the OpenAPI document served at /api/v1/openapi.yaml; nil
	// serves the built-in one.
	Spec []byte
}

type server struct {
	st   store.Store
	opts Options
	log  *slog.Logger
}

// handler is an endpoint: it writes its response, or returns an error for
// the server to report.
type handler func(w http.ResponseWriter, r *http.Request) error

// libHandler is an endpoint on one library, named by {lib}.
type libHandler func(w http.ResponseWriter, r *http.Request, a *app.Service) error

// Route is one endpoint, as the spec lists it.
type Route struct{ Method, Path string }

// New returns the API's handler.
func New(st store.Store, opts Options) http.Handler {
	if opts.Spec == nil {
		opts.Spec = Spec
	}
	s := &server{st: st, opts: opts, log: opts.Logger}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.Handle(rt.method+" "+Prefix+rt.path, s.wrap(rt.h, rt.public))
	}
	mux.Handle(Prefix+"/", s.wrap(func(http.ResponseWriter, *http.Request) error {
		return errNoRoute
	}, false))
	return mux
}

type route struct {
	method, path string
	h            handler
	public       bool
}

// Routes lists every endpoint, so tests can hold them to the spec.
func Routes() []Route {
	var out []Route
	for _, rt := range (&server{}).routes() {
		out = append(out, Route{rt.method, rt.path})
	}
	return out
}

var errNoRoute = errors.New("no such endpoint")

// apiError is an error with its HTTP status and code.
type apiError struct {
	status int
	code   string
	msg    string
}

func (e apiError) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return apiError{http.StatusBadRequest, "invalid", fmt.Sprintf(format, args...)}
}

// wrap adds authentication, a body limit, and error reporting.
func (s *server) wrap(h handler, public bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "path", r.URL.Path, "panic", v)
				s.fail(w, r, fmt.Errorf("panic: %v", v))
			}
		}()
		if !public && s.opts.Token != "" {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.opts.Token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="rflib"`)
				s.fail(w, r, apiError{http.StatusUnauthorized, "unauthorized", "a valid bearer token is required"})
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := h(w, r); err != nil {
			s.fail(w, r, err)
		}
	})
}

// errorBody is every error response.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// fail reports err with the status its kind calls for. Server errors are
// logged, and their detail kept from the caller.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := http.StatusInternalServerError, "internal", "internal error"
	var ae apiError
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &ae):
		status, code, msg = ae.status, ae.code, ae.msg
	case errors.Is(err, errNoRoute):
		status, code, msg = http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, store.ErrNotFound):
		status, code, msg = http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, store.ErrConflict):
		status, code, msg = http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, app.ErrReadOnly):
		status, code, msg = http.StatusForbidden, "read_only", err.Error()
	case errors.As(err, &tooBig):
		status, code, msg = http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("the request body is over %d bytes", tooBig.Limit)
	case errors.Is(err, domain.ErrInvalid):
		status, code, msg = http.StatusBadRequest, "invalid", err.Error()
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	var body errorBody
	body.Error.Code, body.Error.Message = code, msg
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, `{"error":{"code":"internal","message":"internal error"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// readJSON decodes a request body into v, refusing unknown fields. An empty
// body leaves v as it is.
func readJSON(r *http.Request, v any) error {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("the request body isn't valid JSON for this endpoint: %v", err)
	}
	return nil
}

// lib opens the library named by {lib} for h.
func (s *server) lib(h libHandler) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		a, err := app.Open(r.Context(), s.st, r.PathValue("lib"))
		if err != nil {
			return err
		}
		return h(w, r, a)
	}
}

// query parameters

func queryBool(r *http.Request, name string) (bool, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, badRequest("%s must be true or false", name)
	}
	return b, nil
}

func queryInt(r *http.Request, name string) (*int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, badRequest("%s must be a whole number", name)
	}
	return &n, nil
}

func pathInt(r *http.Request, name string) (int, error) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil {
		return 0, badRequest("%s must be a number", name)
	}
	return n, nil
}

// ctx is the request's context, for brevity in handlers.
func ctx(r *http.Request) context.Context { return r.Context() }
