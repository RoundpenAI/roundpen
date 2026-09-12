package hostsetup

import (
	"strings"

	"github.com/RoundpenAI/roundpen/internal/runtime"
)

// WizardContext is the create-wizard form payload used for planning.
type WizardContext struct {
	Name         string `json:"name"`
	Bio          string `json:"bio"`
	IdentityMode string `json:"identityMode"`
	Preset       string `json:"preset"`
	NetworkTier string `json:"networkTier,omitempty"`
}

// PlannedAction is one whitelist step in a setup plan.
type PlannedAction struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Reason    string    `json:"reason"`
	Sensitive bool      `json:"sensitive"`
	Privilege Privilege `json:"privilege,omitempty"`
	Command   string    `json:"command"`
}

// Plan is a workstation preparation plan.
type Plan struct {
	Summary string          `json:"summary"`
	Actions []PlannedAction `json:"actions"`
}

// HostFacts is the probe outcome used by planners.
type HostFacts struct {
	BinariesOK     bool
	AgentImageOK   bool
	BrowserImageOK bool
}

// DeterministicPlan builds a plan from a runtime snapshot (no LLM).
func DeterministicPlan(ctx WizardContext, snap runtime.Snapshot, priv Privilege) Plan {
	return PlanFromFacts(ctx, factsFromSnapshot(snap), priv)
}

// PlanFromFacts maps host gaps + wizard preset to whitelist actions.
func PlanFromFacts(ctx WizardContext, f HostFacts, priv Privilege) Plan {
	needBrowser := ctx.Preset == "code_browser"
	var actions []PlannedAction

	if !f.BinariesOK {
		actions = append(actions, planned(ActionInstallQEMU, "当前环境缺少 qemu-system-x86_64 / qemu-img", priv))
	}
	if !f.AgentImageOK {
		actions = append(actions, planned(ActionBuildAgentImage, "默认工位镜像尚未构建", priv))
	}
	if needBrowser && !f.BrowserImageOK {
		actions = append(actions, planned(ActionBuildBrowserImage, "浏览器画面环境尚未准备", priv))
	}

	summary := "工位已就绪"
	switch {
	case len(actions) == 0:
		// keep
	case len(actions) == 1:
		summary = "需要：" + actions[0].Title
	default:
		summary = "需要准备本机虚拟机组件与工位镜像"
		if needBrowser {
			summary = "需要本机虚拟机组件、助手系统盘和浏览器画面环境"
		} else if !f.BinariesOK && !f.AgentImageOK {
			summary = "需要本机虚拟机组件和助手系统盘"
		}
	}

	return Plan{Summary: summary, Actions: actions}
}

func planned(id, reason string, priv Privilege) PlannedAction {
	def, _ := LookupAction(id)
	a := PlannedAction{
		ID:        id,
		Title:     def.Title,
		Reason:    reason,
		Sensitive: def.Sensitive,
		Command:   commandFor(id),
	}
	if def.Sensitive {
		a.Privilege = priv
	}
	return a
}

func factsFromSnapshot(snap runtime.Snapshot) HostFacts {
	var f HostFacts
	for _, e := range snap.Engines {
		if e.ID != runtime.EngineQEMU {
			continue
		}
		// Start optimistic; Missing marks gaps. Ready flags force OK.
		f.BinariesOK = true
		f.AgentImageOK = true
		f.BrowserImageOK = true
		for _, m := range e.Missing {
			if strings.Contains(m, "QEMU binaries") {
				f.BinariesOK = false
			}
			if strings.Contains(m, "agent qcow2") {
				f.AgentImageOK = false
			}
			if strings.Contains(m, "browser qcow2") {
				f.BrowserImageOK = false
			}
		}
		if e.AgentReady {
			f.BinariesOK = true
			f.AgentImageOK = true
		}
		if e.BrowserReady {
			f.BinariesOK = true
			f.BrowserImageOK = true
		}
		// If not ready and Missing empty, treat agent path as not ready.
		if !e.AgentReady && len(e.Missing) == 0 {
			f.AgentImageOK = false
			f.BinariesOK = false
		}
		return f
	}
	return HostFacts{}
}
