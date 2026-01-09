package store

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis"
)

const keyPrefix = "pastey_"

// ErrNotFound is returned when a paste doesn't exist or has expired.
var ErrNotFound = errors.New("paste not found")

// Store defines the interface for paste storage operations.
type Store interface {
	// Get retrieves a paste by ID. Returns ErrNotFound if it doesn't exist.
	Get(id string) (string, error)
	// Create attempts to store a paste with the given ID.
	// Returns true if created, false if ID already exists (collision).
	Create(id string, body []byte) (bool, error)
}

// RedisStore implements Store using Redis.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedis creates a new Redis-backed store and verifies connectivity.
func NewRedis(addr, password string, db int, ttl time.Duration) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	if _, err := client.Ping().Result(); err != nil {
		return nil, err
	}

	return &RedisStore{
		client: client,
		ttl:    ttl,
	}, nil
}

// Get retrieves a paste by ID.
func (s *RedisStore) Get(id string) (string, error) {
	val, err := s.client.Get(keyPrefix + id).Result()
	if err == redis.Nil {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// Create stores a paste using SetNX (atomic set-if-not-exists).
// Returns true if the paste was created, false if the ID already exists.
func (s *RedisStore) Create(id string, body []byte) (bool, error) {
	ok, err := s.client.SetNX(keyPrefix+id, string(body), s.ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ParseRedisURI parses a Redis URI in the form "host:port" and returns host and port separately.
// This is needed because the rate limiter package takes host and port as separate config fields.
func ParseRedisURI(uri string) (host string, port int) {
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
