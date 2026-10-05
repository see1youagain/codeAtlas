package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func ID(prefix string) string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Errorf("secure random source unavailable: %w", err))
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

func Token() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Errorf("secure random source unavailable: %w", err))
	}
	return hex.EncodeToString(raw)
}

func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func Digest(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return Hash(string(payload)), nil
}
