package randutil

import (
	"crypto/rand"
	"log/slog"
	"math/big"
)

const alphanum = "123456789abcdefghijklmnopqrstuvwxyz"

// RandString generates a cryptographically random string of length n
// using an unbiased selection from the alphanum character set.
func RandString(n int) string {
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
