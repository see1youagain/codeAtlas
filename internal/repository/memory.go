package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
)

// CurrentRevision consults Git rather than the cached metadata scan.
func (w *Workspace) CurrentRevision(ctx context.Context) string { return w.revision(ctx) }

func (w *Workspace) FileHash(relative string) (string, error) {
	path, err := w.resolve(relative)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Size() > maxReadableBytes {
		return "", errors.New("memory evidence file is unavailable or too large")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}
