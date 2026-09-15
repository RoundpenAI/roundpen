package sysagent

import (
	"encoding/json"
	"testing"
)

func TestSkillMutates(t *testing.T) {
	cases := []struct {
		args string
		want bool
	}{
		{`{}`, false},
		{`{"action":"invoke","skill":"commit"}`, false},
		{`{"action":"list"}`, false},
		{`{"action":"  install  "}`, true},
		{`{"action":"remove"}`, true},
		{`{"action":"bogus"}`, true},
		{`not json`, false},
	}
	for _, tc := range cases {
		if got := skillMutates(json.RawMessage(tc.args)); got != tc.want {
			t.Errorf("skillMutates(%s) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
