package hook

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/renderorange/knowledge-mcp/internal/diag"
)

func TestRunDebugLogsSkipNoPattern(t *testing.T) {
	var logBuf bytes.Buffer
	l := diag.New(&logBuf)
	l.SetNow(func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) })
	in := strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"grep","tool_input":{}}`)
	var out bytes.Buffer
	if err := Run(in, &out, l); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(logBuf.String(), "hook.skip") || !strings.Contains(logBuf.String(), "reason=no-pattern") {
		t.Errorf("missing skip line: %q", logBuf.String())
	}
	if out.Len() != 0 {
		t.Errorf("output changed: %q", out.String())
	}
}

func TestRunSilentWithoutLogger(t *testing.T) {
	in := strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"grep","tool_input":{}}`)
	var out bytes.Buffer
	if err := Run(in, &out, nil); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("output changed: %q", out.String())
	}
}
