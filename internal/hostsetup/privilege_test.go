package hostsetup

import "testing"

func TestClassifyPrivilegeRoot(t *testing.T) {
	if got := classifyPrivilege(true, false); got != PrivilegeAuto {
		t.Fatalf("got %s", got)
	}
}

func TestClassifyPrivilegeSudoN(t *testing.T) {
	if got := classifyPrivilege(false, true); got != PrivilegeAuto {
		t.Fatalf("got %s", got)
	}
}

func TestClassifyPrivilegeManual(t *testing.T) {
	if got := classifyPrivilege(false, false); got != PrivilegeManual {
		t.Fatalf("got %s", got)
	}
}
