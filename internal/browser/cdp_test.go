// internal/browser/cdp_test.go
package browser

import "testing"

func TestCandidatePaths(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"/chrome", "/chromium", "/"}},
		{"/", []string{"/chrome", "/chromium", "/"}},
		{"/chrome", []string{"/chrome"}},
		{"/chromium", []string{"/chromium"}},
		{"/ws/v2", []string{"/ws/v2"}},
	}
	for _, c := range cases {
		got := candidatePaths(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("candidatePaths(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("candidatePaths(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestBuildWSURL(t *testing.T) {
	cases := []struct {
		endpoint string
		path     string
		token    string
		want     string
	}{
		{"http://10.10.1.3:3000", "/chrome", "", "ws://10.10.1.3:3000/chrome"},
		{"https://cloud.example/", "/chrome", "a b&c", "wss://cloud.example/chrome?token=a+b%26c"},
		{"ws://127.0.0.1:1234/chrome", "/chrome", "tok", "ws://127.0.0.1:1234/chrome?token=tok"},
		{"wss://cloud.example/chrome?foo=1", "/chrome", "tok", "wss://cloud.example/chrome?foo=1&token=tok"},
	}
	for _, c := range cases {
		got, err := buildWSURL(c.endpoint, c.path, c.token)
		if err != nil {
			t.Fatalf("buildWSURL(%q, %q, %q): %v", c.endpoint, c.path, c.token, err)
		}
		if got != c.want {
			t.Fatalf("buildWSURL(%q, %q, %q) = %q, want %q", c.endpoint, c.path, c.token, got, c.want)
		}
	}
}
