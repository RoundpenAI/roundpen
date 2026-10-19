package project

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	SubKindGitHubPR = "github_pr"
	SubKindGitHubCI = "github_ci"
)

// Subscription listens for remote GitHub events for one project.
type Subscription struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	ProjectID   string     `json:"projectId"`
	UserID      string     `json:"userId"`
	Kind        string     `json:"kind"`
	Repo        string     `json:"repo"`
	Secret      string     `json:"-"`
	Enabled     bool       `json:"enabled"`
	LastEventAt *time.Time `json:"lastEventAt,omitempty"`
	LastStatus  string     `json:"lastStatus,omitempty"`
	LastSummary string     `json:"lastSummary,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	// HookPath is the control-plane path clients should register with GitHub.
	HookPath string `json:"hookPath,omitempty"`
	// SecretOnce is returned only on create so the user can paste it into GitHub.
	SecretOnce string `json:"secret,omitempty"`
}

// HookPathFor returns the public webhook path for a subscription key.
func HookPathFor(subKey string) string {
	return "/v1/hooks/github/" + strings.TrimSpace(subKey)
}

func NormalizeSubKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case SubKindGitHubPR, "pr", "pull_request":
		return SubKindGitHubPR, nil
	case SubKindGitHubCI, "ci", "check_run", "check_suite", "workflow_run":
		return SubKindGitHubCI, nil
	default:
		return "", fmt.Errorf("kind must be github_pr or github_ci")
	}
}

func NormalizeRepo(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimPrefix(repo, "http://github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("repo must look like owner/name")
	}
	return parts[0] + "/" + parts[1], nil
}

func NewHookSecret() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// VerifyGitHubSignature checks X-Hub-Signature-256 (sha256=hex).
func VerifyGitHubSignature(secret string, body []byte, header string) bool {
	secret = strings.TrimSpace(secret)
	header = strings.TrimSpace(header)
	if secret == "" || header == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// FormatHookNotice is written into the coordinator transcript.
func FormatHookNotice(kind, repo, summary string) string {
	var b strings.Builder
	b.WriteString("[System] GitHub ")
	switch kind {
	case SubKindGitHubPR:
		b.WriteString("pull request")
	case SubKindGitHubCI:
		b.WriteString("CI")
	default:
		b.WriteString(kind)
	}
	b.WriteString(" event")
	if repo != "" {
		b.WriteString(" for ")
		b.WriteString(repo)
	}
	b.WriteString(".\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\nSummarize for the user; open a PR in the browser for discussion threads (they stay on GitHub).")
	return b.String()
}
