// Package template manages environment image templates (slot=agent|browser|mobile).
package template

import "time"

const (
	DefaultNamespace = "default"
	DefaultTag       = "default"
	EnvdVersion      = "0.0.0-roundpen"
)

// BuildStatus is the status of a template's image; the catalog is seeded from
// configuration and images are built in CI, so every entry reports ready.
type BuildStatus string

const (
	BuildReady BuildStatus = "ready"
)

// Record is a registered template for listing.
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
