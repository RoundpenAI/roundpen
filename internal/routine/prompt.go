package routine

import (
	"fmt"
	"strings"
)

// RenderPrompt is the user message injected into a run session.
func RenderPrompt(c Claim, lastSummary string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This is scheduled run %s of standing routine %s.\n", c.Run.Key, c.Routine.Key)
	fmt.Fprintf(&b, "Title: %s\n", c.Routine.Title)
	if strings.TrimSpace(c.Routine.Brief) != "" {
		fmt.Fprintf(&b, "Brief:\n%s\n", c.Routine.Brief)
	}
	fmt.Fprintf(&b, "Autonomy: %s\n", c.Routine.Autonomy)
	if len(c.Routine.Hosts) > 0 {
		fmt.Fprintf(&b, "Hosts: %s\n", strings.Join(c.Routine.Hosts, ", "))
	}
	state := strings.TrimSpace(string(c.Routine.State))
	if state == "" {
		state = "{}"
	}
	fmt.Fprintf(&b, "State: %s\n", state)
	if strings.TrimSpace(lastSummary) == "" {
		lastSummary = "无"
	}
	fmt.Fprintf(&b, "Previous successful summary: %s\n", lastSummary)
	mins := c.Routine.MaxDurationSec / 60
	if mins < 1 {
		mins = 1
	}
	fmt.Fprintf(&b, "Finish within %d minutes by calling FinishRun. ", mins)
	b.WriteString("If you must pay, submit a form, delete user data, or message anyone outside the delivery channel, call RequestRoutineConfirm and stop.")
	return b.String()
}

// SummaryLine is the one line written into the assistant's primary chat.
func SummaryLine(routineKey, runKey, status, summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = status
	}
	return fmt.Sprintf("%s %s（%s）：%s", routineKey, runKey, status, summary)
}
