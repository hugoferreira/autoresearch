package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrArtifactNotFound = errors.New("artifact not found")
var ErrArtifactAmbiguous = errors.New("artifact sha prefix is ambiguous")

// WriteArtifact stores content at a content-addressed path under
// .research/artifacts/. Returns the sha256 hex and the path relative to the
// .research/ directory (e.g. "artifacts/ab/cdef.../raw.out"). If content with
// the same sha already exists, it is not rewritten.
func (s *Store) WriteArtifact(content []byte, filename string) (sha string, relPath string, err error) {
	if filename == "" {
		filename = "raw.out"
	}
	digest := sha256.Sum256(content)
	sha = hex.EncodeToString(digest[:])
	dir := filepath.Join(s.ArtifactsDir(), sha[:2], sha[2:])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir artifact: %w", err)
	}
	full := filepath.Join(dir, filename)
	if _, statErr := os.Stat(full); errors.Is(statErr, os.ErrNotExist) {
		if err := atomicWrite(full, content); err != nil {
			return "", "", err
		}
	}
	rel, err := filepath.Rel(s.DirPath(), full)
	if err != nil {
		return "", "", err
	}
	return sha, rel, nil
}

// ReadArtifact reads the bytes at a relative artifact path previously returned
// by WriteArtifact.
func (s *Store) ReadArtifact(relPath string) ([]byte, error) {
	full := filepath.Join(s.DirPath(), relPath)
	return os.ReadFile(full)
}

// ArtifactLocation resolves a full sha256 or unambiguous hex prefix to an
// existing artifact on disk. Returns (fullSHA, relative path, absolute path).
// Ambiguous prefixes produce ErrArtifactAmbiguous with the candidate shas in
// the error message; unknown shas produce ErrArtifactNotFound.
func (s *Store) ArtifactLocation(shaOrPrefix string) (sha, rel, abs string, err error) {
	shaOrPrefix = strings.ToLower(strings.TrimSpace(shaOrPrefix))
	if len(shaOrPrefix) < 4 {
		return "", "", "", fmt.Errorf("sha prefix too short (need at least 4 hex chars, got %d)", len(shaOrPrefix))
	}
	for _, r := range shaOrPrefix {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return "", "", "", fmt.Errorf("sha contains non-hex character %q", r)
		}
	}

	bucketName := shaOrPrefix[:2]
	bucket := filepath.Join(s.ArtifactsDir(), bucketName)
	entries, err := os.ReadDir(bucket)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", "", ErrArtifactNotFound
	} else if err != nil {
		return "", "", "", fmt.Errorf("read artifact bucket: %w", err)
	}

	rest := shaOrPrefix[2:]
	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), rest) {
			matches = append(matches, e.Name())
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", "", "", ErrArtifactNotFound
	case 1:
		// ok
	default:
		candidates := make([]string, 0, len(matches))
		for _, m := range matches {
			candidates = append(candidates, bucketName+m[:10]+"…")
		}
		return "", "", "", fmt.Errorf("%w: candidates: %s", ErrArtifactAmbiguous, strings.Join(candidates, ", "))
	}

	sha = bucketName + matches[0]
	subDir := filepath.Join(bucket, matches[0])
	files, err := os.ReadDir(subDir)
	if err != nil {
		return "", "", "", fmt.Errorf("read artifact dir: %w", err)
	}
	var filename string
	for _, f := range files {
		if !f.IsDir() {
			filename = f.Name()
			break
		}
	}
	if filename == "" {
		return "", "", "", fmt.Errorf("artifact %s has no file", sha)
	}
	abs = filepath.Join(subDir, filename)
	rel, err = filepath.Rel(s.DirPath(), abs)
	if err != nil {
		return "", "", "", err
	}
	return sha, rel, abs, nil
}
