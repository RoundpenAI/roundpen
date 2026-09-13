// cmd/browserdriver/main.go
//
// Installs the Playwright driver (no browsers) for local dev and image builds.
package main

import (
	"fmt"
	"os"

	"github.com/mxschmitt/playwright-go"
)

func main() {
	err := playwright.Install(&playwright.RunOptions{
		SkipInstallBrowsers: true,
		Verbose:             true,
		DriverDirectory:     os.Getenv("PLAYWRIGHT_DRIVER_PATH"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "playwright driver install:", err)
		os.Exit(1)
	}
	fmt.Println("playwright driver installed")
}
