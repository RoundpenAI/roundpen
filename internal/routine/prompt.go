package routine

import (
	"fmt"
	"strings"
)

// RenderPrompt is the user message injected into a run session.
func RenderPrompt(c Claim, lastSummary string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This is scheduled run %s of standing routine %s", c.Run.Key, c.Routine.Key)
	if c.Run.SegmentIndex > 1 {
		fmt.Fprintf(&b, " (segment %d, continuing long work)", c.Run.SegmentIndex)
	}
	b.WriteByte('\n')
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
	fmt.Fprintf(&b, "State (checkpoint — resume from here): %s\n", state)
	if strings.TrimSpace(lastSummary) == "" {
		lastSummary = "无"
	}
	fmt.Fprintf(&b, "Previous successful summary: %s\n", lastSummary)
	mins := c.Routine.MaxDurationSec / 60
	if mins < 1 {
		mins = 1
	}
	fmt.Fprintf(&b, "This segment has %d minutes. Persist progress with UpdateRoutineState. ", mins)
	b.WriteString("When the whole job is done, call FinishRun. ")
	b.WriteString("If more work remains after this segment, call FinishRun with continue=true so the next segment starts immediately. ")
	b.WriteString("If you must pay, submit a form, delete user data, or message anyone outside the delivery channel, call RequestRoutineConfirm and stop.")
	return b.String()
}

// RenderContinuePrompt is injected when the next segment reuses the same session.
// History already carries prior tools; this nudge is short.
func RenderContinuePrompt(c Claim, lastSummary string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Continue segment %d of %s / %s on the same session.\n", c.Run.SegmentIndex, c.Routine.Key, c.Run.Key)
	state := strings.TrimSpace(string(c.Routine.State))
	if state == "" {
		state = "{}"
	}
	fmt.Fprintf(&b, "State (checkpoint): %s\n", state)
	if strings.TrimSpace(lastSummary) != "" {
		fmt.Fprintf(&b, "Previous segment summary: %s\n", lastSummary)
	}
	mins := c.Routine.MaxDurationSec / 60
	if mins < 1 {
		mins = 1
	}
	fmt.Fprintf(&b, "This segment has %d minutes. Resume from the checkpoint; do not redo finished work. ", mins)
	b.WriteString("When done, FinishRun; if more remains, FinishRun with continue=true.")
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
