package tools

import (
	"fmt"
	"io/fs"
	"testing"
)

func TestIsSkillNotExist(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{&fs.PathError{Op: "open", Path: "x.md", Err: fs.ErrNotExist}, true},
		{fmt.Errorf("cat /workspace/.roundpen/skills/x.md: No such file or directory"), true},
		{fmt.Errorf("permission denied"), false},
		{fmt.Errorf("connection reset by peer"), false},
	}
	for _, tc := range cases {
		if got := isSkillNotExist(tc.err); got != tc.want {
			t.Errorf("isSkillNotExist(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
