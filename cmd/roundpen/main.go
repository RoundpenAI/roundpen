// Package main is the Roundpen CLI (roundpen).
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: roundpen <version|health>\n")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("roundpen 0.0.1-dev")
	case "health":
		base := getenv("ROUNDPEN_URL", "http://127.0.0.1:9527")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(base + "/health")
		if err != nil {
			fmt.Fprintf(os.Stderr, "health: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("%s %s\n", resp.Status, string(body))
		if resp.StatusCode >= 300 {
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
