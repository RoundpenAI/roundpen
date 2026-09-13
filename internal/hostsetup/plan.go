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
	DockerReady bool
}

// DeterministicPlan builds a plan from host facts (no LLM).
func DeterministicPlan(ctx WizardContext, f HostFacts, priv Privilege) Plan {
	return PlanFromFacts(ctx, f, priv)
}

// PlanFromFacts maps host gaps to whitelist actions.
func PlanFromFacts(_ WizardContext, f HostFacts, priv Privilege) Plan {
	var actions []PlannedAction

	if !f.DockerReady {
		actions = append(actions, planned(ActionInstallDocker, "当前环境缺少 Docker（Agent 槽位需要）", priv))
	}

	summary := "工位已就绪"
	switch len(actions) {
	case 0:
		// keep
	case 1:
		summary = "需要：" + actions[0].Title
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
