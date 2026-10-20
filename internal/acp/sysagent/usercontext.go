package sysagent

import (
	"fmt"
	"strings"
)

// FormatUserContext builds the system-prompt block Agents see about the human.
// Empty fields are omitted; an all-empty profile yields "".
func FormatUserContext(username, fullName, orgName, about, timezone string) string {
	username = strings.TrimSpace(username)
	fullName = strings.TrimSpace(fullName)
	orgName = strings.TrimSpace(orgName)
	about = strings.TrimSpace(about)
	timezone = strings.TrimSpace(timezone)
	if username == "" && fullName == "" && orgName == "" && about == "" && timezone == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("About the user you are helping:")
	if fullName != "" {
		fmt.Fprintf(&b, "\n- Preferred name: %s", fullName)
	}
	if username != "" {
		fmt.Fprintf(&b, "\n- Account: %s", username)
	}
	if orgName != "" {
		fmt.Fprintf(&b, "\n- Organization: %s", orgName)
	}
	if timezone != "" {
		fmt.Fprintf(&b, "\n- Timezone: %s", timezone)
	}
	if about != "" {
		fmt.Fprintf(&b, "\n- Notes from the user:\n%s", about)
	}
	b.WriteString("\nAddress them by their preferred name when you know it. Respect their timezone for scheduling.")
	return b.String()
}
