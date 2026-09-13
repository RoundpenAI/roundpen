// internal/browser/keys_test.go
package browser

import "testing"

func TestNormalizeKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Every alias maps as specified.
		{"enter", "Enter"},
		{"return", "Enter"},
		{"tab", "Tab"},
		{"escape", "Escape"},
		{"esc", "Escape"},
		{"backspace", "Backspace"},
		{"space", " "},
		{"pageup", "PageUp"},
		{"pagedown", "PageDown"},
		{"home", "Home"},
		{"end", "End"},
		{"arrowup", "ArrowUp"},
		{"arrowdown", "ArrowDown"},
		{"arrowleft", "ArrowLeft"},
		{"arrowright", "ArrowRight"},
		{"delete", "Delete"},
		{"del", "Delete"},
		// Aliases are case-insensitive, matching the old chromedp engine.
		{"Enter", "Enter"},
		{"ARROWDOWN", "ArrowDown"},
		// Unlisted keys pass through unchanged.
		{"a", "a"},
		{"Control+a", "Control+a"},
		{"F5", "F5"},
		// Surrounding whitespace is trimmed before lookup.
		{"  Enter  ", "Enter"},
		{"  Control+a  ", "Control+a"},
		// A literal single space is the space-bar key and must survive
		// trimming (the old chromedp engine supported it explicitly).
		{" ", " "},
	}
	for _, c := range cases {
		if got := normalizeKey(c.in); got != c.want {
			t.Errorf("normalizeKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
