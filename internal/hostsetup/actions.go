package hostsetup

import "os"

const (
	ActionInstallQEMU       = "install_qemu"
	ActionBuildAgentImage   = "build_agent_image"
	ActionBuildBrowserImage = "build_browser_image"
)

// ActionDef is a whitelist setup action. Commands are never model-supplied.
type ActionDef struct {
	ID        string
	Title     string
	Sensitive bool
	CommandHint func(isRoot bool) string
}

var catalog = map[string]ActionDef{
	ActionInstallQEMU: {
		ID:        ActionInstallQEMU,
		Title:     "安装本机虚拟机组件",
		Sensitive: true,
		CommandHint: func(isRoot bool) string {
			if isRoot {
				return "apt-get install -y qemu-system-x86 qemu-utils"
			}
			return "sudo apt-get install -y qemu-system-x86 qemu-utils"
		},
	},
	ActionBuildAgentImage: {
		ID:        ActionBuildAgentImage,
		Title:     "准备助手系统盘",
		Sensitive: false,
		CommandHint: func(bool) string {
			return "make agent-image"
		},
	},
	ActionBuildBrowserImage: {
		ID:        ActionBuildBrowserImage,
		Title:     "准备浏览器画面环境",
		Sensitive: false,
		CommandHint: func(bool) string {
			return "make browser-image"
		},
	},
}

func LookupAction(id string) (ActionDef, bool) {
	d, ok := catalog[id]
	return d, ok
}

func commandFor(id string) string {
	d, ok := catalog[id]
	if !ok || d.CommandHint == nil {
		return ""
	}
	return d.CommandHint(os.Geteuid() == 0)
}
