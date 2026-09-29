package diag

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// WrapHandlers decorates every handler with tool.start/tool.done debug
// lines carrying sanitized args. A nil logger returns the map unchanged.
func WrapHandlers(h map[string]server.ToolHandlerFunc, l *Logger) map[string]server.ToolHandlerFunc {
	if l == nil {
		return h
	}
	out := make(map[string]server.ToolHandlerFunc, len(h))
	for name, fn := range h {
		name, fn := name, fn
		out[name] = func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			l.Debugf("tool", "tool.start", "name", name, "args", SanitizeArgs(req.GetArguments()))
			start := time.Now()
			res, err := fn(ctx, req)
			dur := time.Since(start).Round(time.Millisecond)
			ok := err == nil && (res == nil || !res.IsError)
			if err != nil {
				l.Debugf("tool", "tool.done", "name", name, "dur", dur, "ok", ok, "err", err.Error())
			} else if res != nil && res.IsError {
				l.Debugf("tool", "tool.done", "name", name, "dur", dur, "ok", ok, "resultErr", true, "reason", truncate(resultText(res), 200))
			} else {
				l.Debugf("tool", "tool.done", "name", name, "dur", dur, "ok", ok)
			}
			return res, err
		}
	}
	return out
}

// resultText returns the first text item of a tool result, or "".
func resultText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
