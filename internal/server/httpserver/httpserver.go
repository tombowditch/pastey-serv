package httpserver

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	rate "github.com/wallstreetcn/rate/redis"

	"github.com/tombowditch/pastey-serv/internal/config"
	"github.com/tombowditch/pastey-serv/internal/paste"
	"github.com/tombowditch/pastey-serv/internal/store"
	"github.com/tombowditch/pastey-serv/internal/util/randutil"
)

// Server holds dependencies for HTTP handlers.
type Server struct {
	store store.Store
}

// NewHandler creates an HTTP handler with all routes configured.
func NewHandler(s store.Store) http.Handler {
	srv := &Server{store: s}

	r := httprouter.New()
	r.GET("/", srv.indexPage)
	r.GET("/:identifier", srv.getIdentifier)
	r.POST("/create", srv.createPaste)

	return r
}

func (s *Server) indexPage(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(`ig.lc - commandline pastebin

pipe to 'nc ig.lc 9999'

- pastes are stored for 72 hours, after which they are automatically deleted

example
=======

~> echo "hello" | nc ig.lc 9999
https://ig.lc/yourpaste

~> cat /etc/nginx/nginx.conf | nc ig.lc 9999
https://ig.lc/yourpaste

~> cat 100mb.bin | nc ig.lc 9999
too much data`))
}

func (s *Server) getIdentifier(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// Rate limit: 1 request per second per IP
	cip := getClientIP(r)
	limiter := rate.NewLimiter(rate.Every(time.Second), 1, "pastey_http_rl_"+cip)
	if !limiter.Allow() {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limit exceeded (1 request per second)"))
		return
	}

	identifier := ps.ByName("identifier")

	val, err := s.store.Get(identifier)
	if err != nil {
		if err != store.ErrNotFound {
			slog.Error("store get failed", "error", err, "identifier", identifier)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found or expired"))
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(val))
}

func (s *Server) createPaste(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	defer r.Body.Close()

	// Rate limit: 1 paste per 5 seconds per IP
	cip := getClientIP(r)
	limiter := rate.NewLimiter(rate.Every(time.Second*5), 1, "pastey_http_create_rl_"+cip)
	if !limiter.Allow() {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limit exceeded (1 paste per 5 seconds)"))
		return
	}

	// Read body (max 5MB + 1 byte to detect overflow)
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(config.MaxPayloadSize)+1))
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error reading body"))
		return
	}

	// Validate paste content
	if err := paste.Validate(body); err != nil {
		w.Header().Set("Content-Type", "text/plain")
		if ve, ok := err.(*paste.ValidationError); ok {
			w.WriteHeader(ve.StatusCode)
			w.Write([]byte(ve.Message))
		} else {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
		}
		return
	}

	// Determine identifier length
	idLength := paste.IDLength(r.URL.Query().Get("secure") == "true")

	// Generate unique identifier and store atomically
	var identifier string
	for tried := 0; tried < 10; tried++ {
		identifier = randutil.RandString(idLength)
		ok, err := s.store.Create(identifier, body)
		if err != nil {
			slog.Error("store create failed", "error", err)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		if ok {
			slog.Info("created paste via HTTP POST", "identifier", identifier, "remote", cip)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(config.BaseURL + identifier + "\n"))
			return
		}
		// Collision, try again
	}

	// Failed to generate unique identifier after retries
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("could not generate identifier"))
}

// getClientIP extracts the real client IP, checking X-Forwarded-For first.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (first IP is the original client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can be comma-separated list: client, proxy1, proxy2
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
