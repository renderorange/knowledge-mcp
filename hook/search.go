package hook

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/renderorange/knowledge-mcp/knowledge"
	"github.com/renderorange/knowledge-mcp/projects"
)

// Hit is one knowledge-store match for the augment output.
type Hit struct {
	Address string
	ID      string
	Summary string
}

// Search scans, in precedence order: the project store (proj), the org
// knowledge files (org), then the global store. Never the bleve index.
func Search(res *projects.Resolver, proj, org *projects.Ref, pattern string, limit int) []Hit {
	type cand struct {
		addr, id, summary string
		score             int
	}
	var cands []cand
	addEntry := func(addr, id, summary, detail, rule string) {
		cands = append(cands, cand{addr, id, summary, scoreEntry(pattern, summary, detail, rule)})
	}
	if proj != nil && proj.Kind == projects.KindProject {
		for _, cat := range knowledge.ValidCategories() {
			kf, err := knowledge.Load(knowledge.CategoryFilePath(res.AgentsDir(*proj), cat))
			if err != nil {
				continue
			}
			for _, e := range kf.Entries {
				addEntry(proj.Address, e.ID, e.Summary, e.Detail, e.Rule)
			}
		}
	}
	if org != nil {
		for _, file := range []string{"architecture.md", "review.md"} {
			data, err := os.ReadFile(filepath.Join(res.OrgKnowledgeDir(*org), file))
			if err != nil {
				continue
			}
			for _, sec := range knowledge.SplitSections(string(data)) {
				addEntry(org.Address, "org-"+file+"::"+sec[0], file+": "+sec[0], sec[1], "")
			}
		}
	}
	if gref, ok := res.GlobalRef(); ok {
		for _, cat := range knowledge.ValidCategories() {
			kf, err := knowledge.Load(knowledge.CategoryFilePath(res.AgentsDir(gref), cat))
			if err != nil {
				continue
			}
			for _, e := range kf.Entries {
				addEntry(gref.Address, e.ID, e.Summary, e.Detail, e.Rule)
			}
		}
	}

	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if limit > len(cands) {
		limit = len(cands)
	}
	var hits []Hit
	for _, c := range cands[:limit] {
		if c.score <= 0 {
			continue
		}
		hits = append(hits, Hit{Address: c.addr, ID: c.id, Summary: truncate(c.summary, 90)})
	}
	return hits
}

// scoreEntry: whole-pattern substring beats word overlap; zero when no
// overlap at all.
func scoreEntry(pattern, summary, detail, rule string) int {
	p := strings.ToLower(strings.TrimSpace(pattern))
	if p == "" {
		return 0
	}
	text := strings.ToLower(summary + "\n" + detail + "\n" + rule)
	if strings.Contains(text, p) {
		return 100 + strings.Count(text, p)
	}
	n := 0
	for _, tok := range strings.Fields(p) {
		if strings.Contains(text, tok) {
			n++
		}
	}
	return n
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}

// resolveCwd maps a cwd to its project ref (longest matching known project
// path) and its containing org ref (longest org-root prefix). Both may be
// nil.
func resolveCwd(res *projects.Resolver, cwd string) (*projects.Ref, *projects.Ref) {
	canon, ok := projects.CanonicalPath(cwd)
	if !ok {
		return nil, nil
	}
	if ref, ok := res.RefForPath(canon); ok {
		switch ref.Kind {
		case projects.KindProject:
			return &ref, orgOf(res, canon)
		case projects.KindOrg:
			return nil, &ref
		}
	}
	var bestProj *projects.Ref
	for _, ref := range res.Snapshot() {
		if ref.Kind != projects.KindProject {
			continue
		}
		if pathWithin(canon, ref.Path) {
			if bestProj == nil || len(ref.Path) > len(bestProj.Path) {
				copyRef := ref
				bestProj = &copyRef
			}
		}
	}
	if bestProj == nil {
		return nil, nil
	}
	return bestProj, orgOf(res, canon)
}

func orgOf(res *projects.Resolver, canon string) *projects.Ref {
	var best *projects.Ref
	for _, ref := range res.Snapshot() {
		if ref.Kind != projects.KindOrg {
			continue
		}
		if pathWithin(canon, ref.Path) {
			if best == nil || len(ref.Path) > len(best.Path) {
				copyRef := ref
				best = &copyRef
			}
		}
	}
	return best
}

func pathWithin(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
