package tools

import "net/http"

// Profiles mirrored from sysagent — kept as strings here to avoid an import cycle.
const (
	ProfileCoordinator = "coordinator"
	ProfileImplementer = "implementer"
	ProfileReviewer    = "reviewer"
	ProfileTester      = "tester"
)

// SessionSurface is everything RegisterSession needs. A non-empty RunKey marks
// a scheduled run: no shell, no interactive questions, no skills, and browser
// tools only when Autonomy is browse.
// Profile narrows tools for project coordinator / code-dev worker roles.
type SessionSurface struct {
	Roundpen    *RoundpenBinder
	API         *RoundpenHTTP
	SessionID   string
	RunKey      string
	Autonomy    string
	Profile     string
	ProjectKey  string // PRJ-n; default for DelegateProjectWork when omitted
	Browser     *BrowserBinder
	Agent       *AgentBinder
	PreviewZone string
	Web         *WebBinder
	WebHTTP     *http.Client
	Search      *WebSearchBinder
	Google      *GoogleBinder
	MCP         *MCPBinder
}

// RegisterSession installs the System Agent tool surface for one session.
func RegisterSession(reg *Registry, s SessionSurface) {
	if reg == nil {
		return
	}
	routine := s.RunKey != ""
	profile := s.Profile

	if s.Roundpen != nil {
		RegisterRoundpen(reg, s.Roundpen)
	}
	if s.API != nil {
		RegisterIssues(reg, s.API, s.SessionID)
		if profile == ProfileCoordinator {
			RegisterProjects(reg, s.API, s.SessionID, s.ProjectKey)
		} else if !routine {
			// Non-coordinator chats keep project tools for visibility; workers skip
			// delegate to avoid nested queues.
			if profile == "" {
				RegisterProjects(reg, s.API, s.SessionID, s.ProjectKey)
				RegisterRoutines(reg, s.API, s.SessionID)
			}
		}
		if routine {
			RegisterRunTools(reg, s.API, s.SessionID, s.RunKey)
		}
	}

	switch {
	case routine:
		allowBrowser := s.Autonomy == "browse"
		if allowBrowser && s.Browser != nil {
			RegisterBrowser(reg, s.Browser)
		}
		if s.Agent != nil {
			RegisterFiles(reg, s.Agent)
			RegisterReadImage(reg, s.Agent)
			RegisterSearch(reg, s.Agent)
		}
	case profile == ProfileCoordinator:
		registerCoordinatorTools(reg, s)
	case profile == ProfileReviewer:
		registerReviewerTools(reg, s)
	case profile == ProfileTester:
		registerTesterTools(reg, s)
	case profile == ProfileImplementer:
		registerImplementerTools(reg, s)
	default:
		registerDefaultTools(reg, s)
	}
}

func registerDefaultTools(reg *Registry, s SessionSurface) {
	if s.Browser != nil {
		RegisterBrowser(reg, s.Browser)
	}
	if s.Agent != nil {
		RegisterPreviewDomain(reg, s.Agent, s.API, s.PreviewZone)
		RegisterShell(reg, s.Agent)
		RegisterSkill(reg, s.Agent, s.WebHTTP)
		RegisterFiles(reg, s.Agent)
		RegisterReadImage(reg, s.Agent)
		RegisterSearch(reg, s.Agent)
		RegisterGitPR(reg, s.Agent)
	}
	RegisterInteractive(reg)
	if s.Web != nil {
		RegisterWebFetch(reg, s.Web)
	}
	if s.Search != nil {
		RegisterWebSearch(reg, s.Search)
	}
	if s.Google != nil {
		RegisterGoogle(reg, s.Google)
	}
	if s.MCP != nil {
		RegisterMCP(reg, s.MCP)
	}
}

func registerCoordinatorTools(reg *Registry, s SessionSurface) {
	// Plan + delegate only: read/search the tree. No Bash/Write/Edit — cloning,
	// installs, builds, and edits must go through DelegateProjectWork.
	if s.Agent != nil {
		RegisterExploreFiles(reg, s.Agent)
		RegisterReadImage(reg, s.Agent)
		RegisterSearch(reg, s.Agent)
	}
	RegisterInteractive(reg)
	if s.Web != nil {
		RegisterWebFetch(reg, s.Web)
	}
	if s.Search != nil {
		RegisterWebSearch(reg, s.Search)
	}
}

func registerImplementerTools(reg *Registry, s SessionSurface) {
	if s.Agent != nil {
		RegisterShell(reg, s.Agent)
		RegisterSkill(reg, s.Agent, s.WebHTTP)
		RegisterFiles(reg, s.Agent)
		RegisterReadImage(reg, s.Agent)
		RegisterSearch(reg, s.Agent)
		RegisterGitPR(reg, s.Agent)
	}
	RegisterInteractive(reg)
	if s.Web != nil {
		RegisterWebFetch(reg, s.Web)
	}
}

func registerReviewerTools(reg *Registry, s SessionSurface) {
	if s.Agent != nil {
		RegisterExploreFiles(reg, s.Agent)
		RegisterReadImage(reg, s.Agent)
		RegisterSearch(reg, s.Agent)
		RegisterShell(reg, s.Agent)
	}
	RegisterInteractive(reg)
}

func registerTesterTools(reg *Registry, s SessionSurface) {
	if s.Agent != nil {
		RegisterShell(reg, s.Agent)
		RegisterFiles(reg, s.Agent)
		RegisterReadImage(reg, s.Agent)
		RegisterSearch(reg, s.Agent)
	}
	RegisterInteractive(reg)
}
