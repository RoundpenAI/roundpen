package tools

import "net/http"

// SessionSurface is everything RegisterSession needs. A non-empty RunKey marks
// a scheduled run: no shell, no interactive questions, no skills, and browser
// tools only when Autonomy is browse.
type SessionSurface struct {
	Roundpen    *RoundpenBinder
	API         *RoundpenHTTP
	SessionID   string
	RunKey      string
	Autonomy    string
	Browser     *BrowserBinder
	Agent       *AgentBinder
	PreviewZone string
	Web         *WebBinder
	WebHTTP     *http.Client
	Search      *WebSearchBinder
}

// RegisterSession installs the System Agent tool surface for one session.
func RegisterSession(reg *Registry, s SessionSurface) {
	if reg == nil {
		return
	}
	routine := s.RunKey != ""
	if s.Roundpen != nil {
		RegisterRoundpen(reg, s.Roundpen)
	}
	if s.API != nil {
		RegisterIssues(reg, s.API, s.SessionID)
		RegisterRoutines(reg, s.API, s.SessionID)
		if routine {
			RegisterRunTools(reg, s.API, s.SessionID, s.RunKey)
		}
	}
	allowBrowser := !routine || s.Autonomy == "browse"
	if allowBrowser && s.Browser != nil {
		RegisterBrowser(reg, s.Browser)
	}
	if s.Agent != nil {
		if !routine {
			RegisterPreviewDomain(reg, s.Agent, s.API, s.PreviewZone)
			RegisterShell(reg, s.Agent)
			RegisterSkill(reg, s.Agent, s.WebHTTP)
		}
		RegisterFiles(reg, s.Agent)
		RegisterSearch(reg, s.Agent)
	}
	if !routine {
		RegisterInteractive(reg)
	}
	if s.Web != nil {
		RegisterWebFetch(reg, s.Web)
	}
	if s.Search != nil {
		RegisterWebSearch(reg, s.Search)
	}
}
