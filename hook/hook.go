package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/renderorange/knowledge-mcp/install"
	"github.com/renderorange/knowledge-mcp/internal/diag"
	"github.com/renderorange/knowledge-mcp/projects"
)

// pluginEvent is the JSON the opencode plugin sends on stdin.
type pluginEvent struct {
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

// Run executes hook-augment: resolve the cwd project from the recorded
// install config and print matching knowledge entries for the tool's
// pattern. Any failure leaves output empty — a hook must never break the
// client tool it augments. When log is non-nil, scan/skip decisions are
// logged to it (stderr → the client's log).
func Run(in io.Reader, out io.Writer, log *diag.Logger) error {
	data, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return err
	}
	var ev pluginEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	pattern, ok := ev.ToolInput["pattern"].(string)
	if !ok || strings.TrimSpace(pattern) == "" {
		log.Debugf("hook", "hook.skip", "reason", "no-pattern")
		return nil
	}

	rec, ok := install.LoadRecord()
	if !ok {
		log.Debugf("hook", "hook.skip", "reason", "no-install-record")
		return nil
	}
	res, _, err := projects.BuildWithStore(rec.Roots, rec.Projects, rec.Global, rec.Store)
	if err != nil {
		log.Debugf("hook", "hook.skip", "reason", "resolve-error", "err", err.Error())
		return nil
	}

	cwd := os.Getenv("KNM_HOOK_CWD")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	proj, org := resolveCwd(res, cwd)

	hits := Search(res, proj, org, pattern, 3, log)
	if len(hits) == 0 {
		log.Debugf("hook", "hook.skip", "reason", "no-hits", "pattern", pattern)
		return nil
	}

	fmt.Fprintf(out, "Knowledge store hits for %q:\n", pattern)
	for _, h := range hits {
		fmt.Fprintf(out, "- %s/%s: %s — query_knowledge(%q, project=%q) for detail\n",
			h.Address, h.ID, h.Summary, pattern, h.Address)
	}
	return nil
}
