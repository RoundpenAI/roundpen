package hostsetup

import (
	"os"
	"os/exec"
	"sync"
	"time"
)

type Privilege string

const (
	PrivilegeAuto   Privilege = "auto"
	PrivilegeManual Privilege = "manual"
)

func classifyPrivilege(isRoot, sudoNOK bool) Privilege {
	if isRoot || sudoNOK {
		return PrivilegeAuto
	}
	return PrivilegeManual
}

var (
	privMu     sync.Mutex
	privCached Privilege
	privAt     time.Time
)

// ProbePrivilege returns auto if euid==0 or `sudo -n true` succeeds.
// Result cached ~30s. Never prompts for a password.
func ProbePrivilege() Privilege {
	privMu.Lock()
	defer privMu.Unlock()
	if time.Since(privAt) < 30*time.Second && privCached != "" {
		return privCached
	}
	isRoot := os.Geteuid() == 0
	sudoNOK := false
	if !isRoot {
		if _, err := exec.LookPath("sudo"); err == nil {
			cmd := exec.Command("sudo", "-n", "true")
			sudoNOK = cmd.Run() == nil
		}
	}
	privCached = classifyPrivilege(isRoot, sudoNOK)
	privAt = time.Now()
	return privCached
}
