package config

import (
	"reflect"
	"testing"
)

func TestSplitKanikoMirrors(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"https://docker.1ms.run", []string{"docker.1ms.run"}},
		{"http://docker.1ms.run/", []string{"docker.1ms.run"}},
		{"docker.1ms.run, mirror.gcr.io", []string{"docker.1ms.run", "mirror.gcr.io"}},
		{"docker.1ms.run mirror.gcr.io", []string{"docker.1ms.run", "mirror.gcr.io"}},
	}
	for _, tt := range tests {
		got := SplitKanikoMirrors(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("SplitKanikoMirrors(%q)=%v want %v", tt.in, got, tt.want)
		}
	}
}
