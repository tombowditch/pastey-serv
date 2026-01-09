package paste

import (
	"net/http"
	"strings"

	"github.com/tombowditch/pastey-serv/internal/config"
)

// ValidationError holds validation failure details.
type ValidationError struct {
	StatusCode int
	Message    string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Validate checks if the paste body is acceptable.
// Returns nil if valid, or a *ValidationError with appropriate status code and message.
func Validate(body []byte) error {
	if len(body) == 0 {
		return &ValidationError{
			StatusCode: http.StatusBadRequest,
			Message:    "empty body",
		}
	}

	if len(body) > config.MaxPayloadSize {
		return &ValidationError{
			StatusCode: http.StatusRequestEntityTooLarge,
			Message:    "payload too big",
		}
	}

	content := string(body)
	for _, phrase := range config.BlacklistedPhrases {
		if strings.Contains(content, phrase) {
			return &ValidationError{
				StatusCode: http.StatusForbidden,
				Message:    "blacklisted phrases, antispam system\ncontact admin@ig.lc if this is in error",
			}
		}
	}

	return nil
}

// IDLength returns the appropriate ID length based on whether secure mode is requested.
func IDLength(secure bool) int {
	if secure {
		return config.IDLengthSecure
	}
	return config.IDLength
}
