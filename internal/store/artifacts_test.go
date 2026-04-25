package store_test

import (
	"errors"

	"github.com/bytter/autoresearch/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("artifact store", func() {
	It("writes content-addressed artifacts and deduplicates identical content", func() {
		s, _ := mustCreate()
		content := []byte("hello world\n")
		sha, rel, err := s.WriteArtifact(content, "raw.out")
		Expect(err).NotTo(HaveOccurred())
		Expect(sha).To(HaveLen(64))
		Expect(rel).To(HavePrefix("artifacts/"))

		sha2, rel2, err := s.WriteArtifact(content, "raw.out")
		Expect(err).NotTo(HaveOccurred())
		Expect(sha2).To(Equal(sha))
		Expect(rel2).To(Equal(rel))

		back, err := s.ReadArtifact(rel)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(back)).To(Equal("hello world\n"))
	})

	It("resolves artifacts by full SHA and prefix and validates bad lookups", func() {
		s, _ := mustCreate()
		sha, _, err := s.WriteArtifact([]byte("hello world\n"), "stdout.txt")
		Expect(err).NotTo(HaveOccurred())

		fullSHA, rel, abs, err := s.ArtifactLocation(sha)
		Expect(err).NotTo(HaveOccurred())
		Expect(fullSHA).To(Equal(sha))
		Expect(rel).To(HavePrefix("artifacts/"))
		Expect(abs).NotTo(BeEmpty())

		fullSHA2, _, _, err := s.ArtifactLocation(sha[:10])
		Expect(err).NotTo(HaveOccurred())
		Expect(fullSHA2).To(Equal(sha))

		_, _, _, err = s.ArtifactLocation("dead1234beef5678")
		Expect(errors.Is(err, store.ErrArtifactNotFound)).To(BeTrue())
		_, _, _, err = s.ArtifactLocation("ZZZZ")
		Expect(err).To(MatchError(ContainSubstring("non-hex")))
		_, _, _, err = s.ArtifactLocation("ab")
		Expect(err).To(MatchError(ContainSubstring("too short")))
	})
})
