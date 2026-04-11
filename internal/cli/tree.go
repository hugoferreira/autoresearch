package cli

import (
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
				children := map[string][]*entity.Hypothesis{}
				var roots []*entity.Hypothesis
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

				if w.IsJSON() {
					return w.JSON(buildTreeJSON(roots, children))
				}
				if len(roots) == 0 && len(all) == 0 {
					w.Textln("(no hypotheses)")
					return nil
				}
				for i, r := range roots {
					last := i == len(roots)-1
					renderTree(w, r, children, "", last)
				}
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

func renderTree(w *output.Writer, h *entity.Hypothesis, children map[string][]*entity.Hypothesis, prefix string, last bool) {
	branch := "├── "
	nextPrefix := prefix + "│   "
	if last {
		branch = "└── "
		nextPrefix = prefix + "    "
	}
	marker := statusGlyph(h.Status)
	w.Textf("%s%s%s %-8s  %s\n", prefix, branch, marker, h.ID, truncate(h.Claim, 72))
	kids := children[h.ID]
	for i, c := range kids {
		renderTree(w, c, children, nextPrefix, i == len(kids)-1)
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
