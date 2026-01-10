package client

import "fmt"

// ErrorCode represents the type of error that occurred.
type ErrorCode int

const (
	// ErrUnknown is an unknown error.
	ErrUnknown ErrorCode = iota
	// ErrEmptyContent is returned when trying to create a paste with empty content.
	ErrEmptyContent
	// ErrPayloadTooLarge is returned when the content exceeds the maximum size.
	ErrPayloadTooLarge
	// ErrRateLimited is returned when rate limited by the server.
	ErrRateLimited
	// ErrNotFound is returned when a paste doesn't exist or has expired.
	ErrNotFound
	// ErrBlacklisted is returned when content contains blacklisted phrases.
	ErrBlacklisted
	// ErrBadRequest is returned for invalid requests.
	ErrBadRequest
	// ErrServer is returned for server-side errors.
	ErrServer
)

// Error represents an error from the Pastey API.
type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("pastey: %s", e.Message)
}

// IsNotFound returns true if the error indicates the paste was not found.
func IsNotFound(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == ErrNotFound
	}
	return false
}

// IsRateLimited returns true if the error indicates rate limiting.
func IsRateLimited(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == ErrRateLimited
	}
	return false
}

// IsPayloadTooLarge returns true if the error indicates the payload was too large.
func IsPayloadTooLarge(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == ErrPayloadTooLarge
	}
	return false
}
