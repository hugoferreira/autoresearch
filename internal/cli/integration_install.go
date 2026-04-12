package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func installManagedDoc(projectDir, relPath, content string, dryRun bool) (string, error) {
	if projectDir == "" {
		return "", errors.New("project dir is empty")
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(abs, relPath)
	if dryRun {
		return fullPath, nil
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", fullPath, err)
	}
	return fullPath, nil
}
