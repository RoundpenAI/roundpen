package template

import "github.com/RoundpenAI/roundpen/internal/template/builder"

// BuildSpec is the E2B-aligned template build request body (v2 start build).
type BuildSpec = builder.Spec

// Step is one layer in a template build (E2B TemplateStep).
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

// CreateTemplateRequest is the input for POST /v3/templates.
type CreateTemplateRequest struct {
	Name      string
	Namespace string
	Tag       string
	Tags      []string
	CPUCount  int
	MemoryMB  int
	Public    bool
	Profile   string
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
