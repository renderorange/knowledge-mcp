package eval

import (
	"os"
	"path/filepath"
	"testing"
)

const mockJSONL = `{"transcript": "GIT CHECKPOINT: About to commit. Explicit permission from user: [NO]."}
{"tool_calls": [{"tool": "bash", "argv": ["git", "status"]}]}
{"new_files": []}
{"git_commits": 0}
`

func TestLoadMockEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.jsonl")
	if err := os.WriteFile(path, []byte(mockJSONL), 0644); err != nil {
		t.Fatal(err)
	}
	ev, err := LoadMockEvidence(path)
	if err != nil {
		t.Fatal(err)
	}
	if ev.GitCommits != 0 || len(ev.ToolCalls) != 1 || ev.ToolCalls[0].Tool != "bash" {
		t.Fatalf("ev: %+v", ev)
	}
}

func TestRunMockPilot(t *testing.T) {
	sc, err := LoadScenario("../evals/scenarios/git-checkpoint-before-commit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ev := MockEvidence{
		Transcript: "GIT CHECKPOINT: About to commit.\n",
		ToolCalls:  []ToolCall{{Tool: "bash", Argv: []string{"git", "status"}}},
	}
	root := t.TempDir()
	res := RunMock(sc, ev, root)
	for _, r := range res {
		if !r.Pass {
			t.Fatalf("expected pass: %+v", r)
		}
	}
	ev.GitCommits = 1
	res = RunMock(sc, ev, root)
	sawFail := false
	for _, r := range res {
		if !r.Pass {
			sawFail = true
		}
	}
	if !sawFail {
		t.Fatal("expected git_rev_count failure with GitCommits=1")
	}
}

func TestScenarioCorpusValid(t *testing.T) {
	scs, err := LoadDir("../evals/scenarios")
	if err != nil {
		t.Fatal(err)
	}
	if len(scs) == 0 {
		t.Fatal("no scenarios found")
	}
	for _, sc := range scs {
		if sc.Tier == "real" {
			continue
		}
		p := filepath.Join("../evals/mock_agents", sc.ID+".jsonl")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing mock evidence for %s: %v", sc.ID, err)
		}
	}
}
