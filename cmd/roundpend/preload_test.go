package main

import (
	"reflect"
	"testing"
)

func TestUniqueImageRefs(t *testing.T) {
	got := uniqueImageRefs(
		" ghcr.io/browserless/chrome:v2.56.7 ",
		"",
		"ghcr.io/roundpenai/code-agent:0.1.0",
		"ghcr.io/browserless/chrome:v2.56.7",
		"ghcr.io/roundpenai/code-agent:0.1.0",
		"host",
		"images/browser.qcow2",
	)
	want := []string{
		"ghcr.io/browserless/chrome:v2.56.7",
		"ghcr.io/roundpenai/code-agent:0.1.0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
