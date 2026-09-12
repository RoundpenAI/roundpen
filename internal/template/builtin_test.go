package template

import "testing"

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin(DefaultNamespace, "code-agent", "system") {
		t.Fatal("code-agent should be builtin")
	}
	if IsBuiltin(DefaultNamespace, "my-agent", "") {
		t.Fatal("user template should not be builtin")
	}
	if IsBuiltin(DefaultNamespace, "code-agent", "") {
		t.Fatal("code-agent without system creator is not protected as builtin")
	}
}
