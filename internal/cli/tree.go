package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/bytter/autoresearch/internal/entity"
	"github.com/bytter/autoresearch/internal/output"
	"github.com/spf13/cobra"
)

func treeCommands() []*cobra.Command {
	return []*cobra.Command{
		{
			Use:   "tree",
			Short: "Render the hypothesis tree",
			RunE: func(cmd *cobra.Command, args []string) error {
				w := output.Default(globalJSON)
				s, err := openStore()
				if err != nil {
					return err
				}
				all, err := s.ListHypotheses()
				if err != nil {
					return err
				}
				roots, children := buildHypothesisForest(all)

				if w.IsJSON() {
					return w.JSON(buildTreeJSON(roots, children))
				}
				if len(roots) == 0 && len(all) == 0 {
					w.Textln("(no hypotheses)")
					return nil
				}
				renderForestToWriter(w.Raw(), roots, children, 72, nil)
				return nil
			},
		},
	}
}

type treeNode struct {
	ID       string      `json:"id"`
	Claim    string      `json:"claim"`
	Status   string      `json:"status"`
	Author   string      `json:"author"`
	Children []*treeNode `json:"children,omitempty"`
}

func buildTreeJSON(roots []*entity.Hypothesis, children map[string][]*entity.Hypothesis) []*treeNode {
	var build func(h *entity.Hypothesis) *treeNode
	build = func(h *entity.Hypothesis) *treeNode {
		n := &treeNode{ID: h.ID, Claim: h.Claim, Status: h.Status, Author: h.Author}
		for _, c := range children[h.ID] {
			n.Children = append(n.Children, build(c))
		}
		return n
	}
	out := make([]*treeNode, 0, len(roots))
	for _, r := range roots {
		out = append(out, build(r))
	}
	return out
}

// buildHypothesisForest partitions a flat list into (roots, children map)
// with deterministic ID-sorted ordering at every level.
func buildHypothesisForest(all []*entity.Hypothesis) (roots []*entity.Hypothesis, children map[string][]*entity.Hypothesis) {
	children = map[string][]*entity.Hypothesis{}
	for _, h := range all {
		if h.Parent == "" {
			roots = append(roots, h)
		} else {
			children[h.Parent] = append(children[h.Parent], h)
		}
	}
	sortHyps := func(hs []*entity.Hypothesis) {
		sort.Slice(hs, func(i, j int) bool { return hs[i].ID < hs[j].ID })
	}
	sortHyps(roots)
	for _, v := range children {
		sortHyps(v)
	}
	return roots, children
}

// renderForestToWriter prints every root (and recursively its subtree) as an
// ASCII tree to w. claimWidth clamps the claim column so wide trees stay in
// their lane. Pass a non-nil colorizer to tint the status glyph; pass nil for
// plain ASCII (what the `tree` command uses — callers that pipe output want
// stable bytes).
func renderForestToWriter(w io.Writer, roots []*entity.Hypothesis, children map[string][]*entity.Hypothesis, claimWidth int, a *ansi) {
	for i, r := range roots {
		renderTreeNode(w, r, children, "", i == len(roots)-1, claimWidth, a)
	}
}

func renderTreeNode(w io.Writer, h *entity.Hypothesis, children map[string][]*entity.Hypothesis, prefix string, last bool, claimWidth int, a *ansi) {
	branch := "├── "
	nextPrefix := prefix + "│   "
	if last {
		branch = "└── "
		nextPrefix = prefix + "    "
	}
	marker := coloredStatusGlyph(a, h.Status)
	claim := truncate(h.Claim, claimWidth)
	fmt.Fprintf(w, "%s%s%s %-8s  %s\n", prefix, branch, marker, h.ID, claim)
	kids := children[h.ID]
	for i, c := range kids {
		renderTreeNode(w, c, children, nextPrefix, i == len(kids)-1, claimWidth, a)
	}
}

func statusGlyph(status string) string {
	switch status {
	case entity.StatusSupported:
		return "✓"
	case entity.StatusRefuted:
		return "✗"
	case entity.StatusInconclusive:
		return "?"
	case entity.StatusKilled:
		return "☠"
	default:
		return "•"
	}
}

// coloredStatusGlyph returns the plain glyph wrapped in a status-appropriate
// color when a is non-nil and enabled; otherwise identical to statusGlyph.
func coloredStatusGlyph(a *ansi, status string) string {
	g := statusGlyph(status)
	if a == nil || !a.enabled {
		return g
	}
	switch status {
	case entity.StatusSupported:
		return a.green(g)
	case entity.StatusRefuted:
		return a.red(g)
	case entity.StatusInconclusive:
		return a.yellow(g)
	case entity.StatusKilled:
		return a.dim(g)
	default:
		return a.cyan(g)
	}
}
