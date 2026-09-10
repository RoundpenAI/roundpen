package runtime

import (
	"os/exec"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/backend/qemu"
	"github.com/RoundpenAI/roundpen/internal/config"
)

// SetupStep is one install/build action shown when an engine is not ready.
type SetupStep struct {
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	Command string `json:"command,omitempty"`
}

// EngineStatus is one selectable runtime (QEMU / Docker / Kern).
type EngineStatus struct {
	ID           string      `json:"id"`
	Label        string      `json:"label"`
	Summary      string      `json:"summary"`
	Ready        bool        `json:"ready"`
	AgentReady   bool        `json:"agentReady"`
	BrowserReady bool        `json:"browserReady,omitempty"`
	Missing      []string    `json:"missing,omitempty"`
	Setup        []SetupStep `json:"setup,omitempty"`
}

// Snapshot is the capability report for the settings / ensure UI.
type Snapshot struct {
	DefaultAgentEngine string         `json:"defaultAgentEngine"`
	AgentEngine        string         `json:"agentEngine"`
	Engines            []EngineStatus `json:"engines"`
}

// Probe inspects the host for QEMU binaries, qcow2 images, Docker, and bwrap.
type Probe struct {
	Cfg         *config.Config
	DockerReady bool
	DockerErr   string
}

func (p *Probe) cfg() *config.Config {
	if p != nil && p.Cfg != nil {
		return p.Cfg
	}
	return &config.Config{}
}

func (p *Probe) defaultEngine() string {
	if e := NormalizeEngine(p.cfg().Backend); e != "" {
		return e
	}
	return EngineQEMU
}

func (p *Probe) agentImage() string {
	if v := strings.TrimSpace(p.cfg().AgentImage); v != "" {
		return v
	}
	return "images/agent-qemu/out/agent.qcow2"
}

func (p *Probe) browserImage() string {
	if v := strings.TrimSpace(p.cfg().BrowserImage); v != "" {
		return v
	}
	return "images/browser-qemu/out/browser.qcow2"
}

// Snapshot builds a live capability report. Qcow2 presence is re-checked each call.
func (p *Probe) Snapshot() Snapshot {
	return Snapshot{
		DefaultAgentEngine: p.defaultEngine(),
		Engines:            []EngineStatus{p.qemuStatus(), p.dockerStatus(), p.kernStatus()},
	}
}

// RequireAgent returns NotReady when the engine cannot run an Agent slot.
func (p *Probe) RequireAgent(engine string) error {
	engine = NormalizeEngine(engine)
	if engine == "" {
		engine = p.defaultEngine()
	}
	for _, e := range p.Snapshot().Engines {
		if e.ID != engine {
			continue
		}
		if e.AgentReady {
			return nil
		}
		return &NotReady{Engine: engine, Message: missingMessage(e), Setup: e.Setup}
	}
	return &NotReady{Engine: engine, Message: engine + " is not available"}
}

// RequireBrowser returns NotReady when QEMU cannot run the Browser slot.
func (p *Probe) RequireBrowser() error {
	e := p.qemuStatus()
	if e.BrowserReady {
		return nil
	}
	return &NotReady{Engine: EngineQEMU, Message: missingMessage(e), Setup: e.Setup}
}

func missingMessage(e EngineStatus) string {
	if len(e.Missing) == 0 {
		return e.Label + " is not ready"
	}
	return e.Label + " needs setup: " + strings.Join(e.Missing, "; ")
}

func (p *Probe) qemuStatus() EngineStatus {
	st := EngineStatus{
		ID:      EngineQEMU,
		Label:   "QEMU VM",
		Summary: "Isolated virtual machine. Agent workspace is a virtio disk; Browser is XFCE + Chrome.",
	}
	var missing []string
	var setup []SetupStep
	if err := qemu.BinariesAvailable(); err != nil {
		missing = append(missing, "QEMU binaries (qemu-system-x86_64, qemu-img)")
		setup = append(setup, SetupStep{
			Title:   "Install QEMU",
			Detail:  err.Error(),
			Command: "sudo apt install qemu-system-x86 qemu-utils",
		})
	}
	if err := qemu.ValidateImage(p.agentImage()); err != nil {
		missing = append(missing, "agent qcow2 image")
		setup = append(setup, SetupStep{
			Title:   "Build the Agent VM image",
			Detail:  err.Error(),
			Command: "make agent-image",
		})
	} else {
		st.AgentReady = qemu.BinariesAvailable() == nil
	}
	if err := qemu.ValidateImage(p.browserImage()); err != nil {
		missing = append(missing, "browser qcow2 image")
		setup = append(setup, SetupStep{
			Title:   "Build the Browser VM image",
			Detail:  err.Error(),
			Command: "make browser-image",
		})
	} else {
		st.BrowserReady = qemu.BinariesAvailable() == nil
	}
	st.Missing = missing
	st.Setup = setup
	st.Ready = st.AgentReady || st.BrowserReady
	return st
}

func (p *Probe) dockerStatus() EngineStatus {
	st := EngineStatus{
		ID:      EngineDocker,
		Label:   "Docker",
		Summary: "OCI containers (runc, Kata, or a remote Docker host). Fits language images like Python and Node.",
	}
	if p != nil && p.DockerReady {
		st.Ready = true
		st.AgentReady = true
		return st
	}
	detail := "Docker Engine is not reachable from this process."
	if p != nil && strings.TrimSpace(p.DockerErr) != "" {
		detail = p.DockerErr
	}
	st.Missing = []string{"Docker Engine"}
	st.Setup = []SetupStep{
		{
			Title:   "Install and start Docker",
			Detail:  detail,
			Command: "sudo apt install docker.io && sudo systemctl enable --now docker",
		},
		{
			Title:   "Build the coding-agent image",
			Detail:  "Used when the Agent slot runs on Docker.",
			Command: "docker build -t roundpen-code-agent:local images/code-agent",
		},
	}
	return st
}

func (p *Probe) kernStatus() EngineStatus {
	st := EngineStatus{
		ID:      EngineKern,
		Label:   "Host jail (kern)",
		Summary: "Daemonless bubblewrap jail on this machine. Lightest option; weaker isolation than a VM.",
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		st.Missing = []string{"bubblewrap (bwrap)"}
		st.Setup = []SetupStep{{
			Title:   "Install bubblewrap",
			Detail:  err.Error(),
			Command: "sudo apt install bubblewrap",
		}}
		return st
	}
	st.Ready = true
	st.AgentReady = true
	return st
}
