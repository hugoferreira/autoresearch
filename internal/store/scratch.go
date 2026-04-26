package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
)

var ErrScratchNotFound = errors.New("scratch not found")

func (s *Store) scratchPath(id string) string {
	return filepath.Join(s.ScratchDir(), id+".md")
}

func (s *Store) ReadScratch(id string) (*entity.Scratch, error) {
	path := s.scratchPath(id)
	sc, err := s.scratchCache.getOrLoad(path, func(p string) (*entity.Scratch, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read scratch: %w", err)
		}
		return entity.ParseScratch(data)
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrScratchNotFound
	}
	return sc, err
}

func (s *Store) WriteScratch(sc *entity.Scratch) error {
	if err := os.MkdirAll(s.ScratchDir(), 0o755); err != nil {
		return fmt.Errorf("create scratch dir: %w", err)
	}
	data, err := sc.Marshal()
	if err != nil {
		return fmt.Errorf("encode scratch: %w", err)
	}
	path := s.scratchPath(sc.ID)
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	s.scratchCache.drop(path)
	return nil
}

func (s *Store) ListScratch() ([]*entity.Scratch, error) {
	entries, err := os.ReadDir(s.ScratchDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list scratch: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(ids)
	out := make([]*entity.Scratch, 0, len(ids))
	for _, id := range ids {
		sc, err := s.ReadScratch(id)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		out = append(out, sc)
	}
	return out, nil
}
