package builder

// Spec is the template build request body.
type Spec struct {
	FromImage    string `json:"fromImage,omitempty"`
	FromTemplate string `json:"fromTemplate,omitempty"`
	Force        bool   `json:"force,omitempty"`
	Steps        []Step `json:"steps,omitempty"`
	StartCmd     string `json:"startCmd,omitempty"`
	ReadyCmd     string `json:"readyCmd,omitempty"`
	CPUCount     int    `json:"cpuCount,omitempty"`
	MemoryMB     int    `json:"memoryMB,omitempty"`
	// KeepImageCmd leaves the base image ENTRYPOINT/CMD unchanged (browser MCP images).
	KeepImageCmd bool `json:"keepImageCmd,omitempty"`
}

// Step is one layer in a template build.
type Step struct {
	Type      string   `json:"type"`
	Args      []string `json:"args,omitempty"`
	FilesHash string   `json:"filesHash,omitempty"`
	Force     bool     `json:"force,omitempty"`
}
