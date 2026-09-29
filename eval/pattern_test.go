package eval

import (
	"regexp"
	"testing"
)

// TestCorpusPatternsCompile pins RE2 compatibility for every transcript
// pattern in the corpus. The Go mock tier compiles patterns with
// regexp.MustCompile at assert time — a lookahead/lookbehind authored for
// Perl compatibility would only surface at runtime there. Compiling the whole
// corpus in a test makes the dual-engine constraint (Perl + RE2) a gate.
func TestCorpusPatternsCompile(t *testing.T) {
	scs, err := LoadDir("../evals/scenarios")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scs) == 0 {
		t.Fatal("no scenarios found")
	}
	checked := 0
	for _, sc := range scs {
		for i, ta := range sc.Assert.Transcript {
			if ta.Pattern == "" {
				t.Errorf("%s: transcript[%d] has empty pattern", sc.ID, i)
				continue
			}
			if _, err := regexp.Compile(ta.Pattern); err != nil {
				t.Errorf("%s: transcript[%d] pattern does not compile under RE2: %v", sc.ID, i, err)
				continue
			}
			checked++
		}
	}
	if checked == 0 {
		t.Error("no transcript patterns found in corpus")
	}
}
