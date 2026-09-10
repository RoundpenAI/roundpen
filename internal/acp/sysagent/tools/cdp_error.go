package tools

import (
	"errors"
	"fmt"
	"strings"
)

// InfraError is a broken environment, not a page-level failure. Callers should
// not retry the same browser_* tool.
type InfraError struct {
	Kind string
	Msg  string
}

func (e *InfraError) Error() string {
	if e == nil {
		return "infrastructure error"
	}
	return e.Msg
}

// WrapBrowserEnsure rewrites CDP attach failures into a stable, non-retryable error.
func WrapBrowserEnsure(err error) error {
	if err == nil {
		return nil
	}
	var infra *InfraError
	if errors.As(err, &infra) {
		return err
	}
	msg := err.Error()
	if !isCDPInfraMessage(msg) {
		return err
	}
	return &InfraError{
		Kind: "cdp_unavailable",
		Msg: "browser CDP is down: guest Chrome DevTools on :9222 is not reachable. " +
			"This is the Browser sandbox, not a page problem. " +
			"Do not retry browser_* until the environment is up. " +
			"Use roundpen_ensure_browser, wait for the VM, then try again. " +
			"(" + shortInfra(msg) + ")",
	}
}

func isCDPInfraMessage(msg string) bool {
	s := strings.ToLower(msg)
	switch {
	case strings.Contains(s, "nothing listening on guest"),
		strings.Contains(s, "not serving devtools"),
		strings.Contains(s, "cdp attach"),
		strings.Contains(s, "chrome not found"),
		strings.Contains(s, "browser hub not configured"),
		strings.Contains(s, "connection reset"),
		strings.Contains(s, "env cdp"):
		return true
	default:
		return false
	}
}

func shortInfra(msg string) string {
	msg = strings.TrimSpace(msg)
	if i := strings.Index(msg, "Get \""); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	if len(msg) > 160 {
		return msg[:160]
	}
	return msg
}

// InfraKind extracts a stable class from a tool result string.
func InfraKind(result string) string {
	s := strings.ToLower(result)
	if strings.Contains(s, "cdp_unavailable") ||
		(strings.Contains(s, "browser cdp is down")) ||
		(strings.Contains(s, "cdp") && (strings.Contains(s, "nothing listening") ||
			strings.Contains(s, "cdp attach") ||
			strings.Contains(s, "connection reset") ||
			strings.Contains(s, "chrome not found") ||
			strings.Contains(s, "env cdp"))) {
		return "cdp_unavailable"
	}
	return ""
}

// FormatToolError is what the model sees after a failed Call.
func FormatToolError(err error) string {
	if err == nil {
		return ""
	}
	var infra *InfraError
	if errors.As(err, &infra) {
		return fmt.Sprintf("error: %s", infra.Error())
	}
	return "error: " + err.Error()
}
