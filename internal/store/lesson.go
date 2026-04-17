package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bytter/autoresearch/internal/entity"
)

var ErrLessonNotFound = errors.New("lesson not found")

func (s *Store) lessonPath(id string) string {
	return filepath.Join(s.LessonsDir(), id+".md")
}

func (s *Store) ReadLesson(id string) (*entity.Lesson, error) {
	path := s.lessonPath(id)
	l, err := s.lessonCache.getOrLoad(path, func(p string) (*entity.Lesson, error) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read lesson: %w", err)
		}
		return entity.ParseLesson(data)
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrLessonNotFound
	}
	return l, err
}

// WriteLesson atomically writes a lesson file. The lessons/ directory is
// created lazily so existing .research/ installs do not need a migration.
func (s *Store) WriteLesson(l *entity.Lesson) error {
	if err := os.MkdirAll(s.LessonsDir(), 0o755); err != nil {
		return fmt.Errorf("create lessons dir: %w", err)
	}
	data, err := l.Marshal()
	if err != nil {
		return fmt.Errorf("encode lesson: %w", err)
	}
	path := s.lessonPath(l.ID)
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	s.lessonCache.drop(path)
	return nil
}

func (s *Store) LessonExists(id string) (bool, error) {
	_, err := os.Stat(s.lessonPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// ListLessons returns every lesson in ID order. A missing lessons/ directory
// returns an empty slice (not an error) so pre-M10 installs can call this.
func (s *Store) ListLessons() ([]*entity.Lesson, error) {
	entries, err := os.ReadDir(s.LessonsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list lessons: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(ids)
	out := make([]*entity.Lesson, 0, len(ids))
	for _, id := range ids {
		l, err := s.ReadLesson(id)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", id, err)
		}
		out = append(out, l)
	}
	return out, nil
}

// ListLessonsByScope returns lessons whose Scope matches. Empty scope returns
// all lessons (no filter).
func (s *Store) ListLessonsByScope(scope string) ([]*entity.Lesson, error) {
	all, err := s.ListLessons()
	if err != nil {
		return nil, err
	}
	if scope == "" {
		return all, nil
	}
	out := make([]*entity.Lesson, 0, len(all))
	for _, l := range all {
		if l.Scope == scope {
			out = append(out, l)
		}
	}
	return out, nil
}

// ListLessonsForSubject returns lessons whose Subjects slice contains id. Used
// by the report renderer to attach lessons to a hypothesis and its conclusions.
func (s *Store) ListLessonsForSubject(id string) ([]*entity.Lesson, error) {
	all, err := s.ListLessons()
	if err != nil {
		return nil, err
	}
	out := make([]*entity.Lesson, 0)
	for _, l := range all {
		if slices.Contains(l.Subjects, id) {
			out = append(out, l)
		}
	}
	return out, nil
}
