package store_test

import (
	"errors"
	"strings"

	"github.com/bytter/autoresearch/internal/store"
	"github.com/bytter/autoresearch/internal/testkit"
)

var _ = testkit.Spec("TestWriteArtifact", func(t testkit.T) {
	s, _ := mustCreate(t)
	content := []byte("hello world\n")
	sha, rel, err := s.WriteArtifact(content, "raw.out")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 64 {
		t.Errorf("sha length: %d", len(sha))
	}
	if !strings.HasPrefix(rel, "artifacts/") {
		t.Errorf("rel path should begin artifacts/: %q", rel)
	}

	// Dedup: same content + filename → same path, no error.
	sha2, rel2, err := s.WriteArtifact(content, "raw.out")
	if err != nil {
		t.Fatal(err)
	}
	if sha != sha2 || rel != rel2 {
		t.Errorf("dedup: %q != %q or %q != %q", sha, sha2, rel, rel2)
	}

	back, err := s.ReadArtifact(rel)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != "hello world\n" {
		t.Errorf("read back: %q", back)
	}
})

var _ = testkit.Spec("TestArtifactLocation", func(t testkit.T) {
	s, _ := mustCreate(t)
	sha, _, err := s.WriteArtifact([]byte("hello world\n"), "stdout.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Full sha
	fullSha, rel, abs, err := s.ArtifactLocation(sha)
	if err != nil {
		t.Fatal(err)
	}
	if fullSha != sha {
		t.Errorf("sha: got %q want %q", fullSha, sha)
	}
	if !strings.HasPrefix(rel, "artifacts/") {
		t.Errorf("rel: %q", rel)
	}
	if abs == "" {
		t.Errorf("abs empty")
	}

	// Prefix
	prefix := sha[:10]
	fullSha2, _, _, err := s.ArtifactLocation(prefix)
	if err != nil {
		t.Fatalf("prefix lookup: %v", err)
	}
	if fullSha2 != sha {
		t.Errorf("prefix resolved to wrong sha")
	}

	// Unknown
	if _, _, _, err := s.ArtifactLocation("dead1234beef5678"); !errors.Is(err, store.ErrArtifactNotFound) {
		t.Errorf("expected not found, got %v", err)
	}

	// Non-hex
	if _, _, _, err := s.ArtifactLocation("ZZZZ"); err == nil || !strings.Contains(err.Error(), "non-hex") {
		t.Errorf("expected non-hex error, got %v", err)
	}

	// Too short
	if _, _, _, err := s.ArtifactLocation("ab"); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("expected too-short error, got %v", err)
	}
})
