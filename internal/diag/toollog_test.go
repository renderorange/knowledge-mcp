package diag

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestWrapHandlersLogsStartAndDone(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }
	h := map[string]server.ToolHandlerFunc{
		"write_knowledge": func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		},
	}
	wrapped := WrapHandlers(h, l)
	req := mcp.CallToolRequest{}
	req.Params.Name = "write_knowledge"
	req.Params.Arguments = map[string]any{"project": "eval-sandbox", "detail": "SECRET BODY"}
	_, err := wrapped["write_knowledge"](context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "tool.start name=write_knowledge") {
		t.Errorf("missing tool.start: %q", out)
	}
	if !strings.Contains(out, "tool.done name=write_knowledge") || !strings.Contains(out, "ok=true") {
		t.Errorf("missing tool.done: %q", out)
	}
	if strings.Contains(out, "SECRET") {
		t.Errorf("body leaked: %q", out)
	}
}

func TestWrapHandlersLogsErrors(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }
	h := map[string]server.ToolHandlerFunc{
		"query_knowledge": func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, errors.New("nope")
		},
	}
	wrapped := WrapHandlers(h, l)
	_, _ = wrapped["query_knowledge"](context.Background(), mcp.CallToolRequest{})
	out := buf.String()
	if !strings.Contains(out, "ok=false") || !strings.Contains(out, `err=nope`) {
		t.Errorf("missing error fields: %q", out)
	}
}

func TestWrapHandlersNilLoggerUnchanged(t *testing.T) {
	h := map[string]server.ToolHandlerFunc{
		"f": func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		},
	}
	w := WrapHandlers(h, nil)
	if len(w) != 1 {
		t.Fatalf("len=%d, want 1", len(w))
	}
	_, err := w["f"](context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
}
