package main

import (
	"log/slog"
	"net/http"
	"os"

	rate "github.com/wallstreetcn/rate/redis"

	"github.com/tombowditch/pastey-serv/internal/config"
	"github.com/tombowditch/pastey-serv/internal/server/httpserver"
	"github.com/tombowditch/pastey-serv/internal/server/tcpserver"
	"github.com/tombowditch/pastey-serv/internal/store"
)

func main() {
	redisURI := os.Getenv("REDIS_URI")

	// Initialize store (Redis client with ping check)
	s, err := store.NewRedis(redisURI, config.RedisPassword, config.RedisDB, config.PasteTTL)
	if err != nil {
		slog.Error("could not connect to redis", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to redis")

	// Initialize rate limiter with parsed host/port
	// Note: This creates a separate Redis connection (rate limiter library limitation)
	redisHost, redisPort := store.ParseRedisURI(redisURI)
	if err := rate.SetRedis(&rate.ConfigRedis{
		Host: redisHost,
		Port: redisPort,
		Auth: config.RedisPassword,
	}); err != nil {
		slog.Error("could not initialize rate limiter", "error", err)
		os.Exit(1)
	}

	// Start TCP server
	tcpAddr := config.TCPHost + ":" + config.TCPPort
	tcpSrv := tcpserver.New(s)
	go func() {
		if err := tcpSrv.Serve(tcpAddr); err != nil {
			slog.Error("tcp server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Start HTTP server
	slog.Info("starting http server", "addr", config.HTTPAddr)
	handler := httpserver.NewHandler(s)
	if err := http.ListenAndServe(config.HTTPAddr, handler); err != nil {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
