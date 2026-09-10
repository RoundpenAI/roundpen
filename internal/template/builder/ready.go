package builder

import (
	"fmt"
	"strconv"
	"strings"
)

// ReadyShell converts ready helpers to a shell probe command.
func ReadyShell(ready string) string {
	ready = strings.TrimSpace(ready)
	if ready == "" {
		return "true"
	}
	if strings.HasPrefix(ready, "waitForPort(") || strings.HasPrefix(ready, "wait_for_port(") {
		port := extractParenArg(ready)
		if port == "" {
			return ready
		}
		return fmt.Sprintf("curl -sf http://127.0.0.1:%s/ >/dev/null 2>&1 || nc -z 127.0.0.1 %s", port, port)
	}
	if strings.HasPrefix(ready, "waitForURL(") || strings.HasPrefix(ready, "wait_for_url(") {
		url := extractParenArg(ready)
		if url == "" {
			return ready
		}
		return fmt.Sprintf("curl -sf %q >/dev/null", url)
	}
	if strings.HasPrefix(ready, "waitForFile(") || strings.HasPrefix(ready, "wait_for_file(") {
		path := extractParenArg(ready)
		if path == "" {
			return ready
		}
		return fmt.Sprintf("test -f %q", path)
	}
	if strings.HasPrefix(ready, "waitForProcess(") || strings.HasPrefix(ready, "wait_for_process(") {
		proc := extractParenArg(ready)
		if proc == "" {
			return ready
		}
		return fmt.Sprintf("pgrep -x %q >/dev/null", proc)
	}
	if strings.HasPrefix(ready, "waitForTimeout(") || strings.HasPrefix(ready, "wait_for_timeout(") {
		ms := extractParenArg(ready)
		sec := 1
		if ms != "" {
			if v, err := strconv.Atoi(ms); err == nil {
				sec = (v + 999) / 1000
				if sec < 1 {
					sec = 1
				}
			}
		}
		return fmt.Sprintf("sleep %d", sec)
	}
	return ready
}

func extractParenArg(s string) string {
	i := strings.Index(s, "(")
	j := strings.LastIndex(s, ")")
	if i < 0 || j <= i {
		return ""
	}
	inner := strings.TrimSpace(s[i+1 : j])
	inner = strings.Trim(inner, `"'`)
	if idx := strings.Index(inner, ","); idx >= 0 {
		inner = strings.TrimSpace(inner[:idx])
	}
	return strings.Trim(inner, `"'`)
}
