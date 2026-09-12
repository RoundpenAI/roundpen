package hostsetup

import "os"

const (
	ActionInstallDocker     = "install_docker"
	ActionInstallQEMU       = "install_qemu"
	ActionBuildBrowserImage = "build_browser_image"
)

// ActionDef is a whitelist setup action. Commands are never model-supplied.
type ActionDef struct {
	ID          string
	Title       string
	Sensitive   bool
	CommandHint func(isRoot bool) string
}

var catalog = map[string]ActionDef{
	ActionInstallDocker: {
		ID:        ActionInstallDocker,
		Title:     "安装 Docker 引擎",
		Sensitive: true,
		CommandHint: func(isRoot bool) string {
			if isRoot {
				return "apt-get install -y docker.io && systemctl enable --now docker"
			}
			return "sudo apt-get install -y docker.io && sudo systemctl enable --now docker"
		},
	},
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
