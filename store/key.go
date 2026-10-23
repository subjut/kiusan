package store

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// URLSafeLetters is the alphabet RandomKey draws from.
const URLSafeLetters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"

// RandomKey returns a random string of length n built from URLSafeLetters,
// using crypto/rand.
func RandomKey(n int) (string, error) {
	if n < 0 {
		return "", errors.New("store: RandomKey length must be >= 0")
	}
	max := big.NewInt(int64(len(URLSafeLetters)))
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = URLSafeLetters[idx.Int64()]
	}
	return string(b), nil
}
