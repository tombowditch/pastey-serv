package config

import "time"

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

// BlacklistedPhrases contains spam/attack patterns to reject.
var BlacklistedPhrases = []string{
	"Cookie: mstshash=Administ",
	"-esystem('cmd /c echo .close",
	"md /c echo Set xHttp=createobjec",
}
