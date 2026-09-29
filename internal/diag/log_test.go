package diag

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNilLoggerIsNoop(t *testing.T) {
	var l *Logger
	l.Debugf("tool", "tool.start", "name", "x") // must not panic
}

func TestDebugfFormat(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.SetNow(func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) })
	l.Debugf("tool", "tool.start", "name", "write_knowledge", "ok", true)
	got := buf.String()
	want := "2026-09-27T12:00:00Z DEBUG tool tool.start name=write_knowledge ok=true\n"
	if got != want {
		t.Errorf("Debugf() = %q, want %q", got, want)
	}
}

func TestDebugfQuotesValuesWithSpaces(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.SetNow(func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) })
	l.Debugf("hook", "hook.skip", "err", "boom town")
	if !strings.Contains(buf.String(), `err="boom town"`) {
		t.Errorf("got %q, want quoted value", buf.String())
	}
}

func TestFromEnv(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false}, {"0", false}, {"no", false}, {"2", false},
		{"1", true}, {"true", true}, {"TRUE", true}, {" true ", true},
	}
	for _, c := range cases {
		t.Setenv("KNM_DEBUG", c.val)
		var buf bytes.Buffer
		l := FromEnv(&buf)
		if (l != nil) != c.want {
			t.Errorf("FromEnv(KNM_DEBUG=%q) nil=%v, want enabled=%v", c.val, l == nil, c.want)
		}
	}
}

func TestSanitizeArgsBodiesNeverAppear(t *testing.T) {
	args := map[string]any{
		"project": "eval-sandbox",
		"id":      "conv-001",
		"detail":  "SECRET BODY that must never leak into logs",
		"rule":    "NEVER leak this rule body",
	}
	got := SanitizeArgs(args)
	if strings.Contains(got, "SECRET") || strings.Contains(got, "leak this rule") {
		t.Fatalf("body leaked: %q", got)
	}
	if !strings.Contains(got, "detail=<42 chars>") {
		t.Errorf("missing detail size, got %q", got)
	}
	if !strings.Contains(got, "rule=<25 chars>") {
		t.Errorf("missing rule size, got %q", got)
	}
	if !strings.Contains(got, "project=eval-sandbox") || !strings.Contains(got, "id=conv-001") {
		t.Errorf("missing scalars, got %q", got)
	}
}

func TestSanitizeArgsTruncatesLongScalars(t *testing.T) {
	long := strings.Repeat("a", 60)
	got := SanitizeArgs(map[string]any{"query": long})
	if strings.Contains(got, long) {
		t.Errorf("untruncated scalar: %q", got)
	}
	if !regexp.MustCompile(`query=a+…\s?\[60 chars\]`).MatchString(got) {
		t.Errorf("want truncation marker, got %q", got)
	}
}

func TestSanitizeArgsUnprintableFallback(t *testing.T) {
	got := SanitizeArgs(map[string]any{"entries": []any{"a", "b"}})
	if !regexp.MustCompile(`entries=<unprintable \d+ bytes>`).MatchString(got) {
		t.Errorf("want unprintable fallback, got %q", got)
	}
}
