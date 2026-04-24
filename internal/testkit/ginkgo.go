package testkit

import (
	"context"
	"io"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// T is the small testing surface shared by the migrated Ginkgo specs and the
// existing test helpers. It deliberately mirrors the testing.T methods used in
// this repository, plus Run for table-style subcases during the migration.
type T interface {
	Cleanup(func())
	Chdir(string)
	Context() context.Context
	Setenv(string, string)
	Error(...any)
	Errorf(string, ...any)
	Fail()
	FailNow()
	Failed() bool
	Fatal(...any)
	Fatalf(string, ...any)
	Helper()
	Log(...any)
	Logf(string, ...any)
	Name() string
	Parallel()
	Skip(...any)
	SkipNow()
	Skipf(string, ...any)
	Skipped() bool
	TempDir() string
	Attr(string, string)
	Output() io.Writer
	Run(string, func(T)) bool
}

type ginkgoT struct {
	t ginkgo.FullGinkgoTInterface
}

// NewT returns a Ginkgo-backed test handle suitable for existing helpers.
func NewT() T {
	return ginkgoT{t: ginkgo.GinkgoT(1)}
}

// Spec registers a single Ginkgo spec while keeping the old helper-friendly
// test handle explicit at the call site.
func Spec(name string, fn func(T)) bool {
	return ginkgo.Describe(name, func() {
		ginkgo.It("runs", func() {
			fn(NewT())
		})
	})
}

func (g ginkgoT) Cleanup(fn func())                 { g.t.Cleanup(fn) }
func (g ginkgoT) Chdir(dir string)                  { g.t.Chdir(dir) }
func (g ginkgoT) Context() context.Context          { return g.t.Context() }
func (g ginkgoT) Setenv(key, value string)          { g.t.Setenv(key, value) }
func (g ginkgoT) Error(args ...any)                 { g.t.Error(args...) }
func (g ginkgoT) Errorf(format string, args ...any) { g.t.Errorf(format, args...) }
func (g ginkgoT) Fail()                             { g.t.Fail() }
func (g ginkgoT) FailNow()                          { g.t.FailNow() }
func (g ginkgoT) Failed() bool                      { return g.t.Failed() }
func (g ginkgoT) Fatal(args ...any)                 { g.t.Fatal(args...) }
func (g ginkgoT) Fatalf(format string, args ...any) { g.t.Fatalf(format, args...) }
func (g ginkgoT) Helper()                           { g.t.Helper() }
func (g ginkgoT) Log(args ...any)                   { g.t.Log(args...) }
func (g ginkgoT) Logf(format string, args ...any)   { g.t.Logf(format, args...) }
func (g ginkgoT) Name() string                      { return g.t.Name() }
func (g ginkgoT) Parallel()                         { g.t.Parallel() }
func (g ginkgoT) Skip(args ...any)                  { g.t.Skip(args...) }
func (g ginkgoT) SkipNow()                          { g.t.SkipNow() }
func (g ginkgoT) Skipf(format string, args ...any)  { g.t.Skipf(format, args...) }
func (g ginkgoT) Skipped() bool                     { return g.t.Skipped() }
func (g ginkgoT) TempDir() string                   { return g.t.TempDir() }
func (g ginkgoT) Attr(key, value string)            { g.t.Attr(key, value) }
func (g ginkgoT) Output() io.Writer                 { return g.t.Output() }

func (g ginkgoT) Run(name string, fn func(T)) bool {
	ginkgo.By(name)
	fn(g)
	return !g.Failed()
}
