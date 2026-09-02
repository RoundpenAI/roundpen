package template

import "testing"

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin(DefaultNamespace, "host", "system") {
		t.Fatal("host should be builtin")
	}
	if IsBuiltin(DefaultNamespace, "my-agent", "") {
		t.Fatal("user template should not be builtin")
	}
	if IsBuiltin(DefaultNamespace, "host", "") {
		t.Fatal("host without system creator is not protected as builtin")
	}
}
