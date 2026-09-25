package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type MockEvidence struct {
	Transcript string
	ToolCalls  []ToolCall
	NewFiles   []string
	GitCommits int
}

func LoadMockEvidence(path string) (MockEvidence, error) {
	var ev MockEvidence
	f, err := os.Open(path)
	if err != nil {
		return ev, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return ev, fmt.Errorf("%s: %w", path, err)
		}
		if v, ok := row["transcript"]; ok {
			var s string
			json.Unmarshal(v, &s)
			ev.Transcript += s + "\n"
		}
		if v, ok := row["tool_calls"]; ok {
			var tc []ToolCall
			json.Unmarshal(v, &tc)
			ev.ToolCalls = append(ev.ToolCalls, tc...)
		}
		if v, ok := row["new_files"]; ok {
			var nf []string
			json.Unmarshal(v, &nf)
			ev.NewFiles = append(ev.NewFiles, nf...)
		}
		if v, ok := row["git_commits"]; ok {
			json.Unmarshal(v, &ev.GitCommits)
		}
	}
	return ev, sc.Err()
}

func RunMock(sc *Scenario, ev MockEvidence, root string) []Result {
	before := Manifest{}
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(root, p)
			before[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	for _, nf := range ev.NewFiles {
		writeMockFile(root, nf)
	}
	results := CheckTranscript(sc, ev.Transcript)
	results = append(results, CheckSideEffects(sc, root, before)...)
	results = append(results, CheckToolCalls(sc, ev.ToolCalls)...)
	for _, se := range sc.Assert.SideEffects {
		if se.Type == "git_rev_count_unchanged" {
			results = append(results, Result{
				Name:   "git_rev_count_unchanged",
				Pass:   ev.GitCommits == 0,
				Detail: fmt.Sprintf("mock git_commits=%d", ev.GitCommits),
			})
		}
	}
	return results
}

func writeMockFile(root, rel string) {
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte("mock"), 0644)
}
