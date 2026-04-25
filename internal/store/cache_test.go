package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bytter/autoresearch/internal/entity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newTestStore() *Store {
	GinkgoHelper()
	s, err := Create(GinkgoT().TempDir(), Config{
		Build: CommandSpec{Command: "true"},
		Test:  CommandSpec{Command: "true"},
	})
	Expect(err).NotTo(HaveOccurred())
	return s
}

func cacheHypothesis(id, claim string) *entity.Hypothesis {
	return &entity.Hypothesis{
		ID:     id,
		Claim:  claim,
		Status: entity.StatusOpen,
		Predicts: entity.Predicts{
			Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1,
		},
		KillIf:    []string{"fails"},
		CreatedAt: time.Now().UTC(),
	}
}

var _ = Describe("entity cache", func() {
	It("returns the same pointer on read hits", func() {
		s := newTestStore()
		Expect(s.WriteHypothesis(cacheHypothesis("H-0001", "test"))).To(Succeed())

		first, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())
		second, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(first == second).To(BeTrue())
	})

	It("invalidates cached entities after store writes", func() {
		s := newTestStore()
		h := cacheHypothesis("H-0001", "v1")
		Expect(s.WriteHypothesis(h)).To(Succeed())
		_, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())

		h.Claim = "v2"
		Expect(s.WriteHypothesis(h)).To(Succeed())
		got, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Claim).To(Equal("v2"))
	})

	It("reloads cached entities after an external mtime bump", func() {
		s := newTestStore()
		h := cacheHypothesis("H-0001", "v1")
		Expect(s.WriteHypothesis(h)).To(Succeed())
		_, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())

		path := s.hypothesisPath("H-0001")
		h.Claim = "hand-edited"
		data, err := h.Marshal()
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		future := time.Now().Add(2 * time.Second)
		Expect(os.Chtimes(path, future, future)).To(Succeed())

		got, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Claim).To(Equal("hand-edited"))
	})

	It("drops missing files from the cache", func() {
		s := newTestStore()
		Expect(s.WriteHypothesis(cacheHypothesis("H-0001", "v1"))).To(Succeed())
		_, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())

		Expect(os.Remove(s.hypothesisPath("H-0001"))).To(Succeed())
		_, err = s.ReadHypothesis("H-0001")
		Expect(err).To(MatchError(ErrHypothesisNotFound))
	})

	It("returns independent state copies", func() {
		s := newTestStore()
		st1, err := s.State()
		Expect(err).NotTo(HaveOccurred())
		st1.Counters["H"] = 99

		st2, err := s.State()
		Expect(err).NotTo(HaveOccurred())
		Expect(st2.Counters["H"]).NotTo(Equal(99))
	})

	It("supports incremental event reads by byte offset", func() {
		s := newTestStore()
		for i := 0; i < 3; i++ {
			Expect(s.AppendEvent(Event{Kind: "test.tick", Subject: "X"})).To(Succeed())
		}

		all, off, err := s.EventsSince(0)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(3))
		Expect(off).To(BeNumerically(">", 0))

		empty, off2, err := s.EventsSince(off)
		Expect(err).NotTo(HaveOccurred())
		Expect(empty).To(BeEmpty())
		Expect(off2).To(Equal(off))

		Expect(s.AppendEvent(Event{Kind: "test.tick", Subject: "Y"})).To(Succeed())
		tail, _, err := s.EventsSince(off)
		Expect(err).NotTo(HaveOccurred())
		Expect(tail).To(HaveLen(1))
		Expect(tail[0].Subject).To(Equal("Y"))
	})

	It("uses event subjects to invalidate cached entity prefixes", func() {
		s := newTestStore()
		h := cacheHypothesis("H-0001", "v1")
		Expect(s.WriteHypothesis(h)).To(Succeed())
		first, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())

		h.Claim = "v2"
		data, err := h.Marshal()
		Expect(err).NotTo(HaveOccurred())
		path := s.hypothesisPath("H-0001")
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(path, data, 0o644)).To(Succeed())
		Expect(os.Chtimes(path, info.ModTime(), info.ModTime())).To(Succeed())

		s.InvalidateFromEvents([]Event{{Kind: "hypothesis.kill", Subject: "H-0001"}})
		second, err := s.ReadHypothesis("H-0001")
		Expect(err).NotTo(HaveOccurred())
		Expect(second == first).To(BeFalse())
		Expect(second.Claim).To(Equal("v2"))
	})
})

func BenchmarkReadHypothesis_CacheHit(b *testing.B) {
	dir := b.TempDir()
	s, err := Create(dir, Config{
		Build: CommandSpec{Command: "true"},
		Test:  CommandSpec{Command: "true"},
	})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		h := &entity.Hypothesis{
			ID: fmtID("H", i+1), Claim: "x", Status: entity.StatusOpen,
			Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
			KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
		}
		if err := s.WriteHypothesis(h); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := s.ListHypotheses(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ListHypotheses(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadHypothesis_NoCache(b *testing.B) {
	dir := b.TempDir()
	s, err := Create(dir, Config{
		Build: CommandSpec{Command: "true"},
		Test:  CommandSpec{Command: "true"},
	})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		h := &entity.Hypothesis{
			ID: fmtID("H", i+1), Claim: "x", Status: entity.StatusOpen,
			Predicts: entity.Predicts{Instrument: "x", Target: "y", Direction: "decrease", MinEffect: 0.1},
			KillIf:   []string{"fails"}, CreatedAt: time.Now().UTC(),
		}
		if err := s.WriteHypothesis(h); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now := time.Now().Add(time.Duration(i) * time.Second)
		entries, _ := os.ReadDir(s.HypothesesDir())
		for _, e := range entries {
			_ = os.Chtimes(filepath.Join(s.HypothesesDir(), e.Name()), now, now)
		}
		if _, err := s.ListHypotheses(); err != nil {
			b.Fatal(err)
		}
	}
}

func fmtID(prefix string, n int) string {
	digits := "0123456789"
	out := make([]byte, 0, len(prefix)+5)
	out = append(out, prefix...)
	out = append(out, '-')
	buf := [4]byte{'0', '0', '0', '0'}
	for i := 3; i >= 0 && n > 0; i-- {
		buf[i] = digits[n%10]
		n /= 10
	}
	out = append(out, buf[:]...)
	return string(out)
}
