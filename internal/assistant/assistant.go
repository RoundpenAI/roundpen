package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	IdentityProxyUser   = "proxy_user"
	IdentityIndependent = "independent"

	NetworkNone     = "none"
	NetworkDevSites = "dev_sites"
	NetworkAll      = "all"

	StatusActive   = "active"
	StatusDisabled = "disabled"

	KindUser   = "user"
	KindSystem = "system"

	DefaultSystemName = "系统助手"
	DefaultSystemBio  = "常驻入口。可协调其他助手（派活能力后续上线）。"
)

type Capabilities struct {
	Shell   bool `json:"shell"`
	Browser bool `json:"browser"`
	Mobile  bool `json:"mobile"`
	Desktop bool `json:"desktop"`
}

type DirectoryGrant struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"` // read | readwrite
	CreatedAt string `json:"createdAt,omitempty"`
}

type Assistant struct {
	ID               string           `json:"id"`
	UserID           string           `json:"userId"`
	Name             string           `json:"name"`
	Bio              string           `json:"bio"`
	IdentityMode     string           `json:"identityMode"`
	Capabilities     Capabilities     `json:"capabilities"`
	NetworkTier      string           `json:"networkTier"`
	NetworkAllowlist []string         `json:"networkAllowlist"`
	DirectoryGrants  []DirectoryGrant `json:"directoryGrants"`
	Status           string           `json:"status"`
	Kind             string           `json:"kind"`
	PrimarySessionID string           `json:"primarySessionId,omitempty"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

type CreateInput struct {
	Name         string
	Bio          string
	IdentityMode string
	Preset       string // writing | code | code_browser | custom
	Capabilities *Capabilities
	Kind         string // user | system; empty means user
}

func ApplyPreset(preset string) Capabilities {
	switch strings.TrimSpace(preset) {
	case "writing":
		return Capabilities{}
	case "code":
		return Capabilities{Shell: true}
	case "code_browser":
		return Capabilities{Shell: true, Browser: true}
	default:
		return Capabilities{Shell: true}
	}
}

func ValidateCreate(in CreateInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch in.IdentityMode {
	case IdentityProxyUser, IdentityIndependent:
	default:
		return fmt.Errorf("identityMode must be proxy_user or independent")
	}
	switch strings.TrimSpace(in.Kind) {
	case "", KindUser, KindSystem:
	default:
		return fmt.Errorf("kind must be user or system")
	}
	return nil
}

// DefaultSystemCreateInput is the seed for each user's undeletable system assistant.
func DefaultSystemCreateInput() CreateInput {
	return CreateInput{
		Name:         DefaultSystemName,
		Bio:          DefaultSystemBio,
		IdentityMode: IdentityProxyUser,
		Preset:       "code",
		Kind:         KindSystem,
	}
}

// ErrSystemUndeletable is returned when disabling a system assistant.
var ErrSystemUndeletable = errors.New("system assistant cannot be deleted")

func validateDisable(cur *Assistant, status *string) error {
	if status != nil && *status == StatusDisabled && cur != nil && cur.Kind == KindSystem {
		return ErrSystemUndeletable
	}
	return nil
}

func capsJSON(c Capabilities) (json.RawMessage, error) {
	return json.Marshal(c)
}
