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
			if err != nil {
				l.Debugf("tool", "tool.done", "name", name, "dur", dur, "ok", false, "err", err.Error())
			} else {
				l.Debugf("tool", "tool.done", "name", name, "dur", dur, "ok", true)
			}
			return res, err
		}
	}
	return out
}
