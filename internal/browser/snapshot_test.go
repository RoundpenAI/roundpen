// internal/browser/snapshot_test.go
package browser

import "testing"

func TestSnapshotText(t *testing.T) {
	tests := []struct {
		name string
		snap Snapshot
		want string
	}{
		{
			name: "title and url header",
			snap: Snapshot{Title: "Example", URL: "https://example.com/"},
			want: "- Page: Example\n- URL: https://example.com/\n",
		},
		{
			name: "named node",
			snap: Snapshot{
				Title: "T", URL: "https://example.com/",
				Nodes: []SnapNode{{Ref: "e1", Role: "button", Name: "Go", Tag: "button"}},
			},
			want: "- Page: T\n- URL: https://example.com/\n- button \"Go\" [ref=e1]\n",
		},
		{
			name: "empty name falls back to tag",
			snap: Snapshot{
				Title: "T", URL: "https://example.com/",
				Nodes: []SnapNode{{Ref: "e2", Role: "link", Tag: "a"}},
			},
			want: "- Page: T\n- URL: https://example.com/\n- link \"a\" [ref=e2]\n",
		},
		{
			name: "textbox with value gets the suffix",
			snap: Snapshot{
				Title: "T", URL: "https://example.com/",
				Nodes: []SnapNode{{Ref: "e3", Role: "textbox", Name: "Search", Tag: "input", Value: "hello"}},
			},
			want: "- Page: T\n- URL: https://example.com/\n- textbox \"Search\" [ref=e3] value=\"hello\"\n",
		},
		{
			name: "non-textbox value is omitted",
			snap: Snapshot{
				Title: "T", URL: "https://example.com/",
				Nodes: []SnapNode{{Ref: "e4", Role: "combobox", Name: "Pick", Tag: "select", Value: "opt1"}},
			},
			want: "- Page: T\n- URL: https://example.com/\n- combobox \"Pick\" [ref=e4]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := snapshotText(tt.snap); got != tt.want {
				t.Errorf("snapshotText() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}
