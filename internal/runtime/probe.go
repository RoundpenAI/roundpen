package runtime

import (
	"context"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/backend/qemu"
	"github.com/RoundpenAI/roundpen/internal/config"
)

// SetupStep is one install/build action shown when a slot is not ready.
type SetupStep struct {
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	Command string `json:"command,omitempty"`
}

// Snapshot is the Agent (Docker) readiness report for settings / ensure UI.
// Browser readiness lives on the Browser page and uses RequireBrowser.
type Snapshot struct {
	DockerReady bool        `json:"dockerReady"`
	DockerErr   string      `json:"dockerErr,omitempty"`
	AgentImage  string      `json:"agentImage"`
	ImageLocal  bool        `json:"agentImageLocal"`
	Missing     []string    `json:"missing,omitempty"`
	Setup       []SetupStep `json:"setup,omitempty"`
}

// Probe inspects the host for Docker (Agent) and QEMU (Browser) readiness.
type Probe struct {
	Cfg         *config.Config
	DockerReady bool
	DockerErr   string
	// DockerCheck re-pings Docker live; when set it takes precedence over DockerReady.
	DockerCheck func() bool
	// HasImage reports whether an OCI image ref is already present locally.
	HasImage func(ctx context.Context, ref string) bool
}

func (p *Probe) cfg() *config.Config {
	if p != nil && p.Cfg != nil {
		return p.Cfg
	}
	return &config.Config{}
}

func (p *Probe) agentImage() string {
	if v := strings.TrimSpace(p.cfg().AgentImage); v != "" {
		return v
	}
	return "roundpen-code-agent:local"
}

func (p *Probe) browserImage() string {
	if v := strings.TrimSpace(p.cfg().BrowserImage); v != "" {
		return v
	}
	return "images/browser-qemu/out/browser.qcow2"
}

// Snapshot builds a live Agent (Docker) readiness report.
func (p *Probe) Snapshot() Snapshot {
	dockerReady := p != nil && p.DockerReady
	if p != nil && p.DockerCheck != nil {
		dockerReady = p.DockerCheck()
	}
	snap := Snapshot{
		DockerReady: dockerReady,
		AgentImage:  p.agentImage(),
	}
	if p != nil && strings.TrimSpace(p.DockerErr) != "" {
		snap.DockerErr = p.DockerErr
	}
	if snap.DockerReady {
		if p != nil && p.HasImage != nil && p.HasImage(context.Background(), snap.AgentImage) {
			snap.ImageLocal = true
			return snap
		}
		snap.Missing = []string{"Agent image " + snap.AgentImage}
		snap.Setup = []SetupStep{
			{
				Title:   "Pull the Agent image",
				Detail:  "Fetched automatically on first Agent start; run it now to fail fast.",
				Command: "docker pull " + snap.AgentImage,
			},
			{
				Title:   "Offline install",
				Detail:  "On an isolated host, load the OCI archive shipped with the release.",
				Command: "docker load -i code-agent.tar",
			},
		}
		return snap
	}
	snap.Missing = []string{"Docker Engine"}
	snap.Setup = []SetupStep{{
		Title:   "Install and start Docker",
		Detail:  snap.DockerErr,
		Command: "sudo apt install docker.io && sudo systemctl enable --now docker",
	}}
	return snap
}

// RequireAgent returns NotReady when the Agent slot cannot run (Docker down).
// The image itself is pulled lazily by the Docker backend during Ensure.
func (p *Probe) RequireAgent(engine string) error {
	engine = NormalizeEngine(engine)
	if engine == "" {
		engine = EngineDocker
	}
	snap := p.Snapshot()
	if snap.DockerReady {
		return nil
	}
	return &NotReady{
		Engine:  EngineDocker,
		Message: missingMessage(snap.Missing),
		Setup:   snap.Setup,
	}
}

// RequireBrowser returns NotReady when QEMU cannot run the Browser slot.
func (p *Probe) RequireBrowser() error {
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
	if err := qemu.ValidateImage(p.browserImage()); err != nil {
		missing = append(missing, "browser qcow2 image")
		setup = append(setup, SetupStep{
			Title:   "Build the Browser VM image",
			Detail:  err.Error(),
			Command: "make browser-image",
		})
	}
	if len(missing) == 0 {
		return nil
	}
	return &NotReady{
		Engine:  EngineQEMU,
		Message: "QEMU needs setup: " + strings.Join(missing, "; "),
		Setup:   setup,
	}
}

func missingMessage(missing []string) string {
	if len(missing) == 0 {
		return "runtime is not ready"
	}
	return "needs setup: " + strings.Join(missing, "; ")
}
