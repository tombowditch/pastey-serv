package config

import (
	"os"
	"time"
)

const (
	// Server addresses
	TCPHost  = "0.0.0.0"
	TCPPort  = "9999"
	HTTPAddr = "0.0.0.0:3334"

	// Redis defaults
	RedisPassword = ""
	RedisDB       = 0

	// Paste settings
	PasteTTL       = 72 * time.Hour
	MaxPayloadSize = 5_000_000 // 5MB

	// ID lengths
	IDLength       = 7
	IDLengthSecure = 32

	// Base URL for paste links
	BaseURL = "https://ig.lc/"
)

// TrustProxy returns true if X-Forwarded-For and X-Real-IP headers should be trusted.
// Set TRUST_PROXY=true when running behind a reverse proxy (nginx, Cloudflare, etc.).
// Defaults to false for security — untrusted headers can be spoofed to bypass rate limiting.
func TrustProxy() bool {
	return os.Getenv("TRUST_PROXY") == "true"
}

// BlacklistedPhrases contains spam/attack patterns to reject.
var BlacklistedPhrases = []string{
	"Cookie: mstshash=Administ",
	"-esystem('cmd /c echo .close",
	"md /c echo Set xHttp=createobjec",
}
