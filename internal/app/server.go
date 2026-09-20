package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type endpoint func(http.ResponseWriter, *http.Request) error
type bucket struct {
	since time.Time
	count int
}
type Server struct {
	db     *pgxpool.Pool
	cfg    Config
	mu     sync.Mutex
	limits map[string]bucket
	client *http.Client
}

func NewServer(db *pgxpool.Pool, cfg Config) *Server {
	return &Server{db: db, cfg: cfg, limits: map[string]bucket{}, client: &http.Client{Timeout: 12 * time.Second}}
}
func (s *Server) allow(key string, max int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.limits) > 10000 {
		for k, b := range s.limits {
			if now.Sub(b.since) > time.Minute {
				delete(s.limits, k)
			}
		}
		if len(s.limits) > 10000 {
			return false
		}
	}
	b := s.limits[key]
	if now.Sub(b.since) > time.Minute {
		b = bucket{since: now}
	}
	b.count++
	s.limits[key] = b
	return b.count <= max
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return bad("Некорректные данные запроса")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return bad("Некорректные данные запроса")
	}
	return nil
}
func respond(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}
func resourceID(r *http.Request) (int64, error) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || n <= 0 {
		return 0, missing()
	}
	return n, nil
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (s *Server) wrap(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recorded := &statusWriter{ResponseWriter: w}
		w = recorded
		id := randomToken()[:16]
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		started := time.Now()
		err := fn(w, r)
		status := 200
		if err != nil && recorded.status == 0 {
			var p *problem
			if errors.As(err, &p) {
				status = p.Status
			} else {
				p = &problem{500, "internal_error", "Не удалось выполнить запрос"}
				status = 500
				slog.Error("request failed", "request_id", id, "error_type", errorClass(err))
			}
			if status == 429 {
				w.Header().Set("Retry-After", "60")
			}
			_ = respond(w, status, map[string]any{"code": p.Code, "message": p.Message, "request_id": id})
		}
		if recorded.status != 0 {
			status = recorded.status
		}
		slog.Info("request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds())
	}
}
func errorClass(err error) string {
	var p *problem
	if errors.As(err, &p) {
		return p.Code
	}
	return "database_or_internal"
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _ = respond(w, 200, map[string]bool{"ok": true}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if s.db.Ping(ctx) != nil {
			_ = respond(w, 503, map[string]bool{"ok": false})
			return
		}
		_ = respond(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/v1/auth/telegram", s.wrap(func(w http.ResponseWriter, r *http.Request) error {
		if !s.allow("login:"+r.Header.Get("X-Real-IP"), 60) {
			return &problem{429, "rate_limited", "Подождите минуту"}
		}
		return s.login(w, r)
	}))
	routes := []struct {
		pattern   string
		assistant bool
		fn        endpoint
	}{
		{"GET /api/v1/me", false, s.me}, {"PATCH /api/v1/me", false, s.profile},
		{"GET /api/v1/homeworks", false, s.homeworks}, {"GET /api/v1/slots", false, s.listSlots}, {"GET /api/v1/calendar", false, s.calendar}, {"GET /api/v1/slots/{id}", false, s.slotDetails},
		{"POST /api/v1/slots/{id}/bookings", false, s.book}, {"GET /api/v1/me/bookings", false, s.myBookings}, {"POST /api/v1/bookings/{id}/cancel", false, s.cancelBooking},
		{"GET /api/v1/assistant/windows", true, s.listWindows}, {"POST /api/v1/assistant/windows", true, s.createWindow},
		{"GET /api/v1/assistant/windows/{id}", true, s.windowDetails},
		{"PATCH /api/v1/assistant/windows/{id}/comment", true, s.updateWindowComment}, {"POST /api/v1/assistant/windows/{id}/cancel", true, s.cancelWindow}, {"POST /api/v1/assistant/slots/{id}/cancel", true, s.cancelSlot},
	}
	for _, r := range routes {
		mux.HandleFunc(r.pattern, s.wrap(s.authorized(r.assistant, r.fn)))
	}
	for _, route := range []struct {
		pattern string
		fn      endpoint
	}{
		{"PATCH /api/v1/admin/homeworks/{id}", s.updateHomework},
		{"POST /api/v1/admin/homeworks", s.createHomework},
	} {
		mux.HandleFunc(route.pattern, s.wrap(s.adminOnly(route.fn)))
	}
	mux.HandleFunc("POST /telegram/webhook", s.wrap(s.webhook))
	mux.HandleFunc("GET /api/v1/public/config", s.wrap(func(w http.ResponseWriter, r *http.Request) error {
		return respond(w, 200, map[string]string{"bot_username": s.botUsername()})
	}))
	mux.Handle("GET /", staticHandler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://telegram.org; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; object-src 'none'")
		if r.Method != "GET" && r.Method != "HEAD" && strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Origin") != s.cfg.BaseURL {
			_ = respond(w, 403, map[string]string{"code": "origin", "message": "Недопустимый источник запроса"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func page(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit < 1 || limit > 100 {
		limit = 100
	}
	if offset < 0 || offset > 100000 {
		offset = 0
	}
	return limit, offset
}
