package tcpserver

import (
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	rate "github.com/wallstreetcn/rate/redis"

	"github.com/tombowditch/pastey-serv/internal/config"
	"github.com/tombowditch/pastey-serv/internal/paste"
	"github.com/tombowditch/pastey-serv/internal/store"
	"github.com/tombowditch/pastey-serv/internal/util/randutil"
)

// Server holds dependencies for the TCP server.
type Server struct {
	store store.Store
}

// New creates a new TCP server with the given store.
func New(s store.Store) *Server {
	return &Server{store: s}
}

// Serve starts listening on the given address and handles connections.
// This function blocks until the listener fails.
func (s *Server) Serve(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer l.Close()

	slog.Info("tcp server listening", "addr", addr)

	for {
		conn, err := l.Accept()
		if err != nil {
			slog.Error("error accepting connection", "error", err)
			continue
		}
		go s.handleRequest(conn)
	}
}

func (s *Server) handleRequest(conn net.Conn) {
	defer conn.Close()

	msg := make([]byte, 0)
	buf := make([]byte, 1024)
	bytesRead := 0

	// Check rate limit before reading
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

		if bytesRead > config.MaxPayloadSize {
			conn.Write([]byte("payload too big\r\n"))
			return
		}

		msg = append(msg, buf[:n]...)

		conn.SetReadDeadline(time.Now().Add(time.Second * 2))
	}

	// Validate paste content
	if err := paste.Validate(msg); err != nil {
		if ve, ok := err.(*paste.ValidationError); ok {
			// Convert message for TCP (use \r\n line endings)
			tcpMsg := strings.ReplaceAll(ve.Message, "\n", "\r\n")
			conn.Write([]byte(tcpMsg + "\r\n"))
		} else {
			conn.Write([]byte("error\r\n"))
		}
		return
	}

	// Generate unique identifier and store atomically
	var identifier string
	for tried := 0; tried < 10; tried++ {
		identifier = randutil.RandString(config.IDLength)
		ok, err := s.store.Create(identifier, msg)
		if err != nil {
			slog.Error("store create failed", "error", err)
			conn.Write([]byte("error, could not connect to db\r\n"))
			return
		}
		if ok {
			slog.Info("created paste via TCP", "identifier", identifier, "remote", cip)
			conn.Write([]byte(config.BaseURL + identifier + "\r\n"))
			return
		}
		// Collision, try again
		slog.Debug("identifier collision, retrying", "identifier", identifier)
	}

	// Failed to generate unique identifier after retries
	slog.Error("could not generate unique identifier after retries")
	conn.Write([]byte("error\r\n"))
}
