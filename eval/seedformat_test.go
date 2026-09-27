package eval

import (
	"strings"
	"testing"

	"github.com/renderorange/knowledge-mcp/knowledge"
	"gopkg.in/yaml.v3"
)

// TestSeedKnowledgeFilesParse pins the knowledge-store seed format in setup
// fixtures: .agents/<category>.yaml seeds must carry the project:/version:/entries:
// wrapper. A bare YAML list parses as zero entries (knowledge.KnowledgeFile), so
// the store indexes nothing and store-governed scenarios silently lose their rule
// (root cause of the 2026-09-27 wave-2 failures).
func TestSeedKnowledgeFilesParse(t *testing.T) {
	scs, err := LoadDir("../evals/scenarios")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scs) == 0 {
		t.Fatal("no scenarios found")
	}
	checked := 0
	for _, sc := range scs {
		for _, f := range sc.Setup.Files {
			if !strings.HasPrefix(f.Path, ".agents/") || !strings.HasSuffix(f.Path, ".yaml") {
				continue
			}
			if strings.HasSuffix(f.Path, "_meta.yaml") {
				continue
			}
			var kf knowledge.KnowledgeFile
			if err := yaml.Unmarshal([]byte(f.Content), &kf); err != nil {
				t.Errorf("%s: seed %s does not parse as knowledge.KnowledgeFile: %v", sc.ID, f.Path, err)
				continue
			}
			if len(kf.Entries) == 0 {
				t.Errorf("%s: seed %s parses with zero entries (needs project:/version:/entries: wrapper)", sc.ID, f.Path)
				continue
			}
			for _, e := range kf.Entries {
				if e.ID == "" || e.Summary == "" || e.Detail == "" {
					t.Errorf("%s: seed %s entry missing id/summary/detail: %+v", sc.ID, f.Path, e)
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Error("no .agents/ seeds found in corpus; corpus is expected to seed the knowledge store")
	}
}
