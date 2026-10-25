package sysagent

import (
	"strings"
	"testing"
)

func TestSystemPromptForProfile(t *testing.T) {
	cases := map[string][]string{
		ProfileCoordinator: {
			"Project Coordinator", "DelegateProjectWork", "implementer", "no Bash",
			"Forbidden substitutes", "UI/frontend",
		},
		ProfileImplementer: {"Project Implementer", "Do not replan"},
		ProfileReviewer:    {"Project Reviewer", "do not implement"},
		ProfileTester:      {"Project Tester", "pass/fail"},
		ProfileDefault:     {"Roundpen System Agent", "Workspace tools"},
	}
	for profile, needles := range cases {
		got := systemPromptForProfile(profile)
		for _, n := range needles {
			if !strings.Contains(got, n) {
				t.Fatalf("profile %q missing %q", profile, n)
			}
		}
	}
}
