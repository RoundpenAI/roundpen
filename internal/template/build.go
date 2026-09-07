package template

import "github.com/RoundpenAI/roundpen/internal/template/builder"

// BuildSpec is the template build request body.
type BuildSpec = builder.Spec

// Step is one layer in a template build.
type Step = builder.Step

// BuildInfo is a single build record.
type BuildInfo struct {
	BuildID      string
	TemplateID   string
	Status       BuildStatus
	ArtifactRef  string
	CacheKey     string
	Spec         BuildSpec
	StartCmd     string
	ReadyCmd     string
	Snapshot     bool
	ErrorMessage string
	CPUCount     int
	MemoryMB     int
	DiskSizeMB   int
}

// LogEntry is a structured build log line.
type LogEntry struct {
	Seq       int
	Timestamp string
	Level     string
	Message   string
	Step      string
}

// CreateTemplateRequest is the input for POST /v1/templates.
type CreateTemplateRequest struct {
	Name      string
	Namespace string
	Tag       string
	Tags      []string
	CPUCount  int
	MemoryMB  int
	Public    bool
	Profile   string
	Slot      string
	CreatedBy string
}

// CreateTemplateResult is returned when a template + pending build are created.
type CreateTemplateResult struct {
	TemplateID string
	BuildID    string
	Namespace  string
	Name       string
	Tags       []string
	Public     bool
}

// CreateBuildRequest allocates a new waiting build under an existing template.
type CreateBuildRequest struct {
	// Tags are version labels (e.g. v2, staging) pointing at the new build.
	// "default" is reserved — use AssignDefault instead.
	Tags []string
	// AssignDefault moves the default tag to this build when it becomes ready (default true).
	AssignDefault *bool
	CPUCount      int
	MemoryMB      int
	DiskSizeMB    int
}

// CreateBuildResult is returned by CreateBuild.
type CreateBuildResult struct {
	TemplateID string
	BuildID    string
	Tags       []string
}

// StartBuildResult is returned by StartBuild (buildID may differ after auto-fork).
type StartBuildResult struct {
	TemplateID string
	BuildID    string
	// Forked is true when a ready build was replaced by a new build due to core spec change.
	Forked bool
}

// TagInfo is a template tag pointing at a build.
type TagInfo struct {
	Tag     string
	BuildID string
}
