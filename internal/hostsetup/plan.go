package hostsetup

// WizardContext is the create-wizard form payload used for planning.
type WizardContext struct {
	Name         string `json:"name"`
	Bio          string `json:"bio"`
	IdentityMode string `json:"identityMode"`
	Preset       string `json:"preset"`
	NetworkTier  string `json:"networkTier,omitempty"`
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
	DockerReady    bool
	BinariesOK     bool // QEMU binaries for the Browser slot
	BrowserImageOK bool
}

// DeterministicPlan builds a plan from host facts (no LLM).
func DeterministicPlan(ctx WizardContext, f HostFacts, priv Privilege) Plan {
	return PlanFromFacts(ctx, f, priv)
}

// PlanFromFacts maps host gaps + wizard preset to whitelist actions.
func PlanFromFacts(ctx WizardContext, f HostFacts, priv Privilege) Plan {
	needBrowser := ctx.Preset == "code_browser"
	var actions []PlannedAction

	if !f.DockerReady {
		actions = append(actions, planned(ActionInstallDocker, "当前环境缺少 Docker（Agent 槽位需要）", priv))
	}
	if needBrowser && !f.BinariesOK {
		actions = append(actions, planned(ActionInstallQEMU, "当前环境缺少 qemu-system-x86_64 / qemu-img", priv))
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
		summary = "需要准备 Docker 与浏览器环境"
		if !needBrowser {
			summary = "需要准备本机 Docker 环境"
		} else if f.DockerReady && !f.BinariesOK && !f.BrowserImageOK {
			summary = "需要本机虚拟机组件和浏览器画面环境"
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
