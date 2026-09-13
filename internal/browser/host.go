// Host Chrome discovery (the provider=host browser source).
package browser

import (
	"os"
	"os/exec"
	"strings"
)

// ChromeOnPATH reports whether a Chrome/Chromium binary is available to this process.
func ChromeOnPATH() bool {
	return lookupChrome() != ""
}

// ChromePath returns the host Chrome executable path, or "" when absent.
func ChromePath() string { return lookupChrome() }

func lookupChrome() string {
	if p := strings.TrimSpace(os.Getenv("CHROME_PATH")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
