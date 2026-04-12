package integration

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed agents/*.md
var agentFS embed.FS

// AgentFile is one generated subagent prompt.
type AgentFile struct {
	Name     string // e.g. "research-generator"
	Filename string // e.g. "research-generator.md"
	Content  []byte
}

// EmbeddedAgents returns every subagent template baked into the binary,
// in a stable alphabetical order.
func EmbeddedAgents() ([]AgentFile, error) {
	return embeddedAgentFiles(agentFS, "agents")
}

func embeddedAgentFiles(fs embed.FS, dir string) ([]AgentFile, error) {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read embedded agents: %w", err)
	}
	var out []AgentFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := fs.ReadFile(dir + "/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", e.Name(), err)
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		out = append(out, AgentFile{Name: name, Filename: e.Name(), Content: data})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func installAgentFiles(projectDir, relDir string, agents []AgentFile) (AgentInstallResult, error) {
	res := AgentInstallResult{}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return res, err
	}
	dir := filepath.Join(abs, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("create %s: %w", dir, err)
	}
	res.Dir = dir
	for _, a := range agents {
		full := filepath.Join(dir, a.Filename)
		if err := os.WriteFile(full, a.Content, 0o644); err != nil {
			return res, fmt.Errorf("write %s: %w", full, err)
		}
		res.Written = append(res.Written, a.Filename)
	}
	res.Count = len(agents)
	return res, nil
}

func previewAgentFiles(projectDir, relDir string, agents []AgentFile) (AgentInstallResult, error) {
	res := AgentInstallResult{}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return res, err
	}
	res.Dir = filepath.Join(abs, relDir)
	for _, a := range agents {
		res.Written = append(res.Written, a.Filename)
	}
	res.Count = len(agents)
	return res, nil
}

// AgentInstallResult reports what InstallAgents did.
type AgentInstallResult struct {
	Dir     string
	Written []string // filenames we wrote (created or overwritten)
	Count   int
}

// InstallAgents writes every embedded subagent template into
// <projectDir>/.claude/agents/. It creates the directory if absent and
// overwrites existing research-*.md files unconditionally (they are fully
// managed). Non-research agent files in .claude/agents/ are never touched.
func InstallAgents(projectDir string) (AgentInstallResult, error) {
	agents, err := EmbeddedAgents()
	if err != nil {
		return AgentInstallResult{}, err
	}
	return installAgentFiles(projectDir, filepath.Join(".claude", "agents"), agents)
}

// PreviewAgents reports what InstallAgents WOULD write, without mutating.
func PreviewAgents(projectDir string) (AgentInstallResult, error) {
	agents, err := EmbeddedAgents()
	if err != nil {
		return AgentInstallResult{}, err
	}
	return previewAgentFiles(projectDir, filepath.Join(".claude", "agents"), agents)
}
