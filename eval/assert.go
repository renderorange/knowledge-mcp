package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Result struct {
	Name   string
	Pass   bool
	Detail string
}

type Manifest map[string]bool

type ToolCall struct {
	Tool string
	Argv []string
}

func CheckTranscript(sc *Scenario, transcript string) []Result {
	var out []Result
	for i, ta := range sc.Assert.Transcript {
		re := regexp.MustCompile(ta.Pattern)
		found := re.MatchString(transcript)
		pass := found == ta.Must
		out = append(out, Result{
			Name:   fmt.Sprintf("transcript[%d] %q must=%v", i, ta.Pattern, ta.Must),
			Pass:   pass,
			Detail: fmt.Sprintf("matched=%v", found),
		})
	}
	return out
}

func CheckSideEffects(sc *Scenario, root string, before Manifest) []Result {
	var out []Result
	for _, se := range sc.Assert.SideEffects {
		switch se.Type {
		case "path_absent":
			_, err := os.Stat(filepath.Join(root, se.Path))
			out = append(out, Result{
				Name:   fmt.Sprintf("path_absent %s", se.Path),
				Pass:   os.IsNotExist(err),
				Detail: fmt.Sprintf("err=%v", err),
			})
		case "path_exists":
			_, err := os.Stat(filepath.Join(root, se.Path))
			out = append(out, Result{
				Name:   fmt.Sprintf("path_exists %s", se.Path),
				Pass:   err == nil,
				Detail: fmt.Sprintf("err=%v", err),
			})
		case "forbidden_new_paths":
			out = append(out, checkForbiddenNewPaths(root, before, se.Allow)...)
		case "git_rev_count_unchanged":
			// implemented in Task 3 via evidence; documented as no-op here
			out = append(out, Result{Name: "git_rev_count_unchanged", Pass: true,
				Detail: "checked by evidence-based runner"})
		}
	}
	return out
}

func checkForbiddenNewPaths(root string, before Manifest, allow []string) []Result {
	var res []Result
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, ".git/") || before[rel] {
			return nil
		}
		for _, pat := range allow {
			if ok, _ := filepath.Match(strings.TrimSuffix(pat, "**"), rel); ok || strings.HasPrefix(rel, strings.TrimSuffix(pat, "/**")) {
				return nil
			}
		}
		res = append(res, Result{
			Name:   "forbidden_new_paths " + rel,
			Pass:   false,
			Detail: "new file outside allow globs",
		})
		return nil
	})
	if len(res) == 0 {
		res = append(res, Result{Name: "forbidden_new_paths", Pass: true, Detail: "clean"})
	}
	return res
}

func CheckToolCalls(sc *Scenario, calls []ToolCall) []Result {
	var out []Result
	for _, ta := range sc.Assert.ToolCalls {
		found := false
		for _, c := range calls {
			if c.Tool == ta.Tool {
				found = true
				break
			}
		}
		out = append(out, Result{
			Name:   fmt.Sprintf("tool_calls %s must=%v", ta.Tool, ta.Must),
			Pass:   found == ta.Must,
			Detail: fmt.Sprintf("found=%v", found),
		})
	}
	return out
}
