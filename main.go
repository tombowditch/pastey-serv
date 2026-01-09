package main

import (
	"crypto/rand"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/julienschmidt/httprouter"
	rate "github.com/wallstreetcn/rate/redis"
)

const (
	CONN_HOST  = "0.0.0.0"
	CONN_PORT  = "9999"
	CONN_TYPE  = "tcp"
	REDIS_PASS = ""
	REDIS_DB   = 0

	ID_LENGTH        = 7
	ID_LENGTH_SECURE = 32
)

var BLACKLISTED_PHRASES = [...]string{"Cookie: mstshash=Administ", "-esystem('cmd /c echo .close", "md /c echo Set xHttp=createobjec"}

var client *redis.Client

// parseRedisURI parses a Redis URI in the form "host:port" and returns host and port separately.
// This is needed because the rate limiter package takes host and port as separate config fields.
func parseRedisURI(uri string) (host string, port int) {
	host = "localhost"
	port = 6379

	if uri == "" {
		return
	}

	parts := strings.Split(uri, ":")
	if len(parts) >= 1 {
		host = parts[0]
	}
	if len(parts) >= 2 {
		if p, err := strconv.Atoi(parts[1]); err == nil {
			port = p
		}
	}
	return
}

func main() {
	redisURI := os.Getenv("REDIS_URI")

	client = redis.NewClient(&redis.Options{
		Addr:     redisURI,
		Password: REDIS_PASS,
		DB:       REDIS_DB,
	})

	// Initialize rate limiter with parsed host/port
	// Note: This creates a separate Redis connection (rate limiter limitation)
	redisHost, redisPort := parseRedisURI(redisURI)
	if err := rate.SetRedis(&rate.ConfigRedis{
		Host: redisHost,
		Port: redisPort,
		Auth: REDIS_PASS,
	}); err != nil {
		slog.Error("could not initialize rate limiter", "error", err)
		os.Exit(1)
	}

	_, err := client.Ping().Result()
	if err != nil {
		slog.Error("could not connect to redis", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to redis")

	// Start TCP server
	go startTCPServer()

	// Start HTTP server
	slog.Info("starting http server", "addr", "0.0.0.0:3334")

	r := httprouter.New()

	r.GET("/", indexPage)
	r.GET("/:identifier", getIdentifier)
	r.POST("/create", createPaste)

	if err := http.ListenAndServe("0.0.0.0:3334", r); err != nil {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}

func startTCPServer() {
	l, err := net.Listen(CONN_TYPE, CONN_HOST+":"+CONN_PORT)
	if err != nil {
		slog.Error("error listening", "error", err)
		os.Exit(1)
	}
	defer l.Close()
	slog.Info("tcp server listening", "addr", CONN_HOST+":"+CONN_PORT)

	for {
		conn, err := l.Accept()
		if err != nil {
			slog.Error("error accepting connection", "error", err)
			continue
		}
		go handleRequest(conn)
	}
}

// getClientIP extracts the real client IP, checking X-Forwarded-For first
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

func indexPage(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
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

func getIdentifier(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
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

	val, err := client.Get("pastey_" + identifier).Result()
	if err != nil && err != redis.Nil {
		slog.Error("redis get failed", "error", err, "identifier", identifier)
	}

	if val != "" {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(val))
	} else {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found or expired"))
	}
}

func createPaste(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
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

	// Read body (max 5MB)
	body, err := io.ReadAll(io.LimitReader(r.Body, 5000001))
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error reading body"))
		return
	}

	if len(body) > 5000000 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		w.Write([]byte("payload too big"))
		return
	}

	if len(body) == 0 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("empty body"))
		return
	}

	// Check blacklist
	for _, phrase := range BLACKLISTED_PHRASES {
		if strings.Contains(string(body), phrase) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("blacklisted phrases, antispam system\ncontact admin@ig.lc if this is in error"))
			return
		}
	}

	// Determine identifier length: 32 chars if secure=true, otherwise 7
	idLength := ID_LENGTH
	if r.URL.Query().Get("secure") == "true" {
		idLength = ID_LENGTH_SECURE
	}

	// Generate unique identifier and store atomically using SetNX
	var identifier string
	for tried := 0; tried < 10; tried++ {
		identifier = randString(idLength)
		// SetNX atomically sets key only if it doesn't exist, avoiding TOCTOU race
		ok, err := client.SetNX("pastey_"+identifier, string(body), time.Hour*72).Result()
		if err != nil {
			slog.Error("redis setnx failed", "error", err)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		if ok {
			// Successfully stored with unique identifier
			slog.Info("created paste via HTTP POST", "identifier", identifier, "remote", cip)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("https://ig.lc/" + identifier + "\n"))
			return
		}
		// Collision, try again
	}

	// Failed to generate unique identifier after retries
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("could not generate identifier"))
}

func handleRequest(conn net.Conn) {
	defer conn.Close()

	msg := make([]byte, 0)
	buf := make([]byte, 1024)
	bytesRead := 0

	// before we even try to read, are we ratelimited?
	cip := strings.Split(conn.RemoteAddr().String(), ":")[0]
	limiter := rate.NewLimiter(rate.Every(time.Second*5), 5, "pastey_rl_"+cip)
	if !limiter.Allow() {
		slog.Warn("rate limit exceeded", "ip", cip)
		conn.Write([]byte("rate limit exceeded (1 paste per 5 seconds)\r\n"))
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Second * 5))

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); err != io.EOF && (!ok || !netErr.Timeout()) {
				slog.Error("read error", "error", err, "ip", cip)
				conn.Write([]byte("read err\r\n"))
				return
			}
			break
		}

		bytesRead += n

		if bytesRead > 5000000 {
			conn.Write([]byte("payload too big\r\n"))
			return
		}

		msg = append(msg, buf[:n]...)

		conn.SetReadDeadline(time.Now().Add(time.Second * 2))
	}

	if len(msg) == 0 {
		conn.Write([]byte("empty payload\r\n"))
		return
	}

	// check if bad
	for _, phrase := range BLACKLISTED_PHRASES {
		if strings.Contains(string(msg), phrase) {
			conn.Write([]byte("blacklisted phrases, antispam system\r\ncontact admin@ig.lc if this is in error\r\n"))
			return
		}
	}

	// Generate unique identifier and store atomically using SetNX (fixes TOCTOU race)
	var identifier string
	for tried := 0; tried < 10; tried++ {
		identifier = randString(ID_LENGTH)
		// SetNX atomically sets key only if it doesn't exist
		ok, err := client.SetNX("pastey_"+identifier, string(msg), time.Hour*72).Result()
		if err != nil {
			slog.Error("redis setnx failed", "error", err)
			conn.Write([]byte("error, could not connect to db\r\n"))
			return
		}
		if ok {
			// Successfully stored with unique identifier
			slog.Info("created paste via TCP", "identifier", identifier, "remote", cip)
			conn.Write([]byte("https://ig.lc/" + identifier + "\r\n"))
			return
		}
		// Collision, try again
		slog.Debug("identifier collision, retrying", "identifier", identifier)
	}

	// Failed to generate unique identifier after retries
	slog.Error("could not generate unique identifier after retries")
	conn.Write([]byte("error\r\n"))
}

// randString generates a random string of length n using crypto/rand
// without modulo bias
func randString(n int) string {
	const alphanum = "123456789abcdefghijklmnopqrstuvwxyz"
	result := make([]byte, n)
	max := big.NewInt(int64(len(alphanum)))

	for i := 0; i < n; i++ {
		num, err := rand.Int(rand.Reader, max)
		if err != nil {
			// Fallback: this should never happen with crypto/rand
			slog.Error("crypto/rand failed", "error", err)
			result[i] = alphanum[0]
			continue
		}
		result[i] = alphanum[num.Int64()]
	}
	return string(result)
}
