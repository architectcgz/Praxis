package project

import (
	"path/filepath"
	"strings"
)

func normalizeAbsolutePath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}
