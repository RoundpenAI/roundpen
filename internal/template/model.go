// Package template manages environment image templates (slot=agent|browser|mobile).
package template

import "time"

const (
	DefaultNamespace = "default"
	DefaultTag       = "default"
	EnvdVersion      = "0.0.0-roundpen"
)

// BuildStatus is the status of a template build.
type BuildStatus string

const (
	BuildReady    BuildStatus = "ready"
	BuildBuilding BuildStatus = "building"
	BuildWaiting  BuildStatus = "waiting"
	BuildError    BuildStatus = "error"
)

// Record is a template with its latest ready build metadata for listing.
type Record struct {
	TemplateID    string
	BuildID       string
	Namespace     string
	Name          string
	Description   string
	Profile       string
	Slot          string // agent | browser | mobile
	Public        bool
	CPUCount      int
	MemoryMB      int
	DiskSizeMB    int
	EnvdVersion   string
	BuildStatus   BuildStatus
	SpawnCount    int64
	BuildCount    int
	LastSpawnedAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CreatedBy     string
	Aliases       []string // legacy alias list; mirrors names
	Names         []string
}

// Resolved is the outcome of resolving a templateID reference at sandbox create.
type Resolved struct {
	RequestRef  string
	TemplateID  string // internal UUID
	BuildID     string
	Alias       string // user-facing ref, e.g. host or default/python
	Image       string // artifact_ref passed to backend
	Profile     string
	Slot        string // agent | browser | mobile
	CPUCount    int
	MemoryMB    int
	DiskSizeMB  int
	StartCmd    string
	Snapshot    bool // image carries roundpen snapshot entrypoint
	UseImageCmd bool
}
