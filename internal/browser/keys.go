package browser

import "strings"

// keyAliases maps lower-case aliases (as models and the takeover panel send
// them) to Playwright key names. Unlisted keys pass through unchanged so
// single characters and modifier chords like "Control+a" keep working.
var keyAliases = map[string]string{
	"enter":      "Enter",
	"return":     "Enter",
	"tab":        "Tab",
	"escape":     "Escape",
	"esc":        "Escape",
	"backspace":  "Backspace",
	"space":      " ",
	"pageup":     "PageUp",
	"pagedown":   "PageDown",
	"home":       "Home",
	"end":        "End",
	"arrowup":    "ArrowUp",
	"arrowdown":  "ArrowDown",
	"arrowleft":  "ArrowLeft",
	"arrowright": "ArrowRight",
	"delete":     "Delete",
	"del":        "Delete",
}

// normalizeKey resolves a caller-supplied key string to a Playwright key name.
func normalizeKey(key string) string {
	if key == " " {
		return " " // literal space: keep it, TrimSpace would erase it
	}
	k := strings.TrimSpace(key)
	if mapped, ok := keyAliases[strings.ToLower(k)]; ok {
		return mapped
	}
	return k
}
