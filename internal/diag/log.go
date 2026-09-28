// Package diag provides debug logging and the knowledge-mcp debug doctor.
package diag

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Logger writes debug lines as
// "2006-01-02T15:04:05Z07:00 DEBUG component message key=value…". A nil
// *Logger is a valid no-op so optional logging costs no branching at call
// sites.
type Logger struct {
	mu  sync.Mutex
	out io.Writer
	now func() time.Time
}

// New returns a Logger writing to out.
func New(out io.Writer) *Logger {
	return &Logger{out: out, now: time.Now}
}

// FromEnv returns a debug Logger when KNM_DEBUG is "1" or "true"
// (case-insensitive, surrounding space ignored), else nil. Extra sinks are
// built by the caller with io.MultiWriter.
func FromEnv(out io.Writer) *Logger {
	v := strings.TrimSpace(os.Getenv("KNM_DEBUG"))
	if v != "1" && !strings.EqualFold(v, "true") {
		return nil
	}
	return New(out)
}

// Debugf logs one debug line. Nil receivers are no-ops.
func (l *Logger) Debugf(component, msg string, kv ...any) {
	if l == nil {
		return
	}
	var b strings.Builder
	b.WriteString(l.now().Format(time.RFC3339))
	b.WriteString(" DEBUG ")
	b.WriteString(component)
	b.WriteString(" ")
	b.WriteString(msg)
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, " %v=%s", kv[i], formatVal(kv[i+1]))
	}
	b.WriteString("\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	io.WriteString(l.out, b.String())
}

func formatVal(v any) string {
	s := fmt.Sprintf("%v", v)
	if strings.ContainsAny(s, " \t\"") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// bodyKeys are argument keys whose values are entry bodies and must never
// be logged — only their size.
var bodyKeys = map[string]bool{"detail": true, "rule": true, "content": true, "text": true}

// SanitizeArgs renders tool arguments for logs: scalar keys are shown
// truncated, body-like keys are replaced with their size, and
// non-scalar shapes fall back to a byte count.
func SanitizeArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := args[k]
		if bodyKeys[k] {
			parts = append(parts, fmt.Sprintf("%s=<%d chars>", k, len([]rune(fmt.Sprintf("%v", v)))))
			continue
		}
		switch v.(type) {
		case string, bool, float64, int, int64:
			parts = append(parts, fmt.Sprintf("%s=%s", k, truncate(fmt.Sprintf("%v", v), 40)))
		default:
			parts = append(parts, fmt.Sprintf("%s=<unprintable %d bytes>", k, len(fmt.Sprintf("%v", v))))
		}
	}
	return strings.Join(parts, " ")
}

// truncate shortens s to n runes, appending "… [N chars]" with the
// original length when truncation occurred.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + fmt.Sprintf("… [%d chars]", len(r))
}
