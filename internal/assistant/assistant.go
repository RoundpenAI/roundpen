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

	KindUser        = "user"
	KindSystem      = "system"
	KindCoordinator = "coordinator"

	DefaultSystemName = "系统助手"
	DefaultSystemBio  = "常驻入口。可协调其他助手（派活能力后续上线）。"

	DefaultCoordinatorBio = "项目协调者：规划与拆解工作，派给执行助手完成，自己不直接改代码。"
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
	ImChannels       ImChannels       `json:"imChannels"`
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
	// Browser is on by default for every preset; users can turn it off in detail.
	switch strings.TrimSpace(preset) {
	case "writing":
		return Capabilities{Browser: true}
	case "code", "code_browser":
		return Capabilities{Shell: true, Browser: true}
	default:
		return Capabilities{Shell: true, Browser: true}
	}
}

// UnmarshalJSON defaults a missing browser key to on so older rows without the
// field stay effective after the product default flipped. Explicit false stays off.
func (c *Capabilities) UnmarshalJSON(data []byte) error {
	type rawCaps struct {
		Shell   bool  `json:"shell"`
		Browser *bool `json:"browser"`
		Mobile  bool  `json:"mobile"`
		Desktop bool  `json:"desktop"`
	}
	var raw rawCaps
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Shell = raw.Shell
	c.Mobile = raw.Mobile
	c.Desktop = raw.Desktop
	if raw.Browser == nil {
		c.Browser = true
	} else {
		c.Browser = *raw.Browser
	}
	return nil
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
	case "", KindUser, KindSystem, KindCoordinator:
	default:
		return fmt.Errorf("kind must be user, system, or coordinator")
	}
	return nil
}

// DefaultCoordinatorCreateInput seeds the undeletable-from-UI coordinator for a Project.
func DefaultCoordinatorCreateInput(projectName string) CreateInput {
	name := strings.TrimSpace(projectName)
	if name == "" {
		name = "Project"
	}
	return CreateInput{
		Name:         name + " · 协调者",
		Bio:          DefaultCoordinatorBio,
		IdentityMode: IdentityProxyUser,
		Preset:       "code",
		Kind:         KindCoordinator,
	}
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
