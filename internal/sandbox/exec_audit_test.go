package sandbox

import (
	"strings"
	"testing"
)

func TestFormatExecCmd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  []string
		want string
	}{
		{name: "empty", cmd: nil, want: ""},
		{name: "simple", cmd: []string{"true"}, want: "true"},
		{
			name: "quoted arg",
			cmd:  []string{"bash", "-lc", "echo hello"},
			want: `bash -lc "echo hello"`,
		},
		{
			name: "empty arg",
			cmd:  []string{"printf", "%s", ""},
			want: `printf %s ""`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatExecCmd(tc.cmd)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	long := strings.Repeat("x", 600)
	got := formatExecCmd([]string{"echo", long})
	if len(got) != 515 || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncate: len=%d got=%q", len(got), got)
	}
}
