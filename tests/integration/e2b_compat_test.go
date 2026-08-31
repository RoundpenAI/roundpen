package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestE2BCompatibility measures Roundpen API conformance against E2B OpenAPI 0.1.0.
// Run: DATABASE_URL=... go test ./tests/integration/ -run TestE2BCompatibility -v
func TestE2BCompatibility(t *testing.T) {
	h := startKernHarness(t)
	ctx := &compatCtx{}
	var results []compatResult

	for _, c := range e2bCompatCases() {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			v, detail := c.Run(t, h, ctx)
			results = append(results, compatResult{Case: c, Verdict: v, Detail: detail})
			msg := fmt.Sprintf("[%s] %s layer=%s e2b=%s implemented=%v", v, c.ID, c.Layer, c.E2BRef, c.Implemented)
			if detail != "" {
				msg += ": " + detail
			}
			t.Log(msg)

			if c.Implemented {
				switch v {
				case verdictFail:
					t.Errorf("implemented endpoint failed: %s", detail)
				case verdictPartial:
					t.Logf("partial compatibility: %s", detail)
				}
			}
		})
	}

	implScore, fullScore, summary := scoreResults(results)
	layerScores := scoreByLayer(results)

	var b strings.Builder
	b.WriteString("\n========== E2B Compatibility Report ==========\n")
	b.WriteString(fmt.Sprintf("Implemented endpoints score: %.1f%%\n", implScore))
	b.WriteString(fmt.Sprintf("Full spec weighted score:    %.1f%%\n", fullScore))
	b.WriteString(summary + "\n")
	for _, layer := range []string{"platform", "envd", "roundpen-ext"} {
		if s, ok := layerScores[layer]; ok {
			b.WriteString(fmt.Sprintf("  %-12s %.1f%%\n", layer+":", s))
		}
	}
	b.WriteString("\nDetails:\n")
	for _, r := range results {
		line := fmt.Sprintf("  %-40s %-16s", r.Case.ID, r.Verdict)
		if r.Detail != "" {
			line += " " + r.Detail
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("==============================================\n")
	t.Log(b.String())

	// Informational only — compatibility score is reported above, not enforced as CI gate.
}
