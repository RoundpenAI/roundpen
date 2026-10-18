package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

const (
	// githubManifestProviderID marks oauth_states rows created for the
	// GitHub App Manifest handshake (not a real login provider).
	githubManifestProviderID = "_github_manifest"

	githubManifestActionURL  = "https://github.com/settings/apps/new"
	githubManifestConvertFmt = "https://api.github.com/app-manifests/%s/conversions"

	githubManifestCallbackPath = "/v1/auth/oauth/github/manifest/callback"
	githubSettingsPath         = "/admin/settings/oauth"
)

// GitHubAppManifest is the JSON posted to GitHub's "Register a GitHub App
// from a manifest" form. See:
// https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest
type GitHubAppManifest struct {
	Name                  string            `json:"name"`
	URL                   string            `json:"url"`
	RedirectURL           string            `json:"redirect_url"`
	CallbackURLs          []string          `json:"callback_urls"`
	Description           string            `json:"description"`
	Public                bool              `json:"public"`
	DefaultPermissions    map[string]string `json:"default_permissions"`
	RequestOAuthOnInstall bool              `json:"request_oauth_on_install"`
}

// GitHubManifestStart is what the admin UI needs to POST a form to GitHub.
type GitHubManifestStart struct {
	ActionURL   string `json:"actionUrl"`
	Manifest    string `json:"manifest"`
	State       string `json:"state"`
	CallbackURL string `json:"callbackUrl"`
}

type githubAppCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Name         string `json:"name"`
	HTMLURL      string `json:"html_url"`
}

// BuildGitHubAppManifest returns the preconfigured app the admin reviews on
// GitHub. Webhooks are omitted so a LAN / NAS console does not need a
// public hook endpoint. request_oauth_on_install is false because that
// auto-OAuth would bypass our PKCE state.
func BuildGitHubAppManifest(consoleScheme, consoleHost string) GitHubAppManifest {
	base := strings.TrimRight(consoleScheme+"://"+consoleHost, "/")
	return GitHubAppManifest{
		Name:         "Roundpen",
		URL:          base,
		RedirectURL:  base + githubManifestCallbackPath,
		CallbackURLs: []string{base + "/v1/auth/oauth/github/callback"},
		Description:  "Sign in to this Roundpen instance and operate GitHub repositories from the agent sandbox.",
		Public:       true,
		DefaultPermissions: map[string]string{
			"contents":      "write",
			"metadata":      "read",
			"pull_requests": "write",
			"issues":        "write",
			"email":         "read",
		},
		RequestOAuthOnInstall: false,
	}
}

// StartGitHubManifest records a one-time state and returns the form fields
// the admin UI posts to GitHub.
func (s *Service) StartGitHubManifest(ctx context.Context, adminUser, consoleScheme, consoleHost string) (*GitHubManifestStart, error) {
	adminUser = strings.TrimSpace(adminUser)
	if adminUser == "" {
		return nil, fmt.Errorf("admin user is required")
	}
	if consoleHost == "" {
		return nil, fmt.Errorf("console host is required")
	}
	state, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	manifest := BuildGitHubAppManifest(consoleScheme, consoleHost)
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := s.Store.CreateState(ctx, State{
		State:       state,
		ProviderID:  githubManifestProviderID,
		LinkUser:    adminUser,
		RedirectURI: manifest.RedirectURL,
		RedirectTo:  githubSettingsPath,
		ExpiresAt:   now.Add(s.stateTTL()),
		CreatedAt:   now,
	}); err != nil {
		return nil, err
	}
	return &GitHubManifestStart{
		ActionURL:   githubManifestActionURL + "?state=" + state,
		Manifest:    string(raw),
		State:       state,
		CallbackURL: manifest.CallbackURLs[0],
	}, nil
}

// CompleteGitHubManifest exchanges GitHub's temporary code for app
// credentials and upserts the github.com provider.
func (s *Service) CompleteGitHubManifest(ctx context.Context, code, state, sessionUser string) (*Provider, error) {
	if strings.TrimSpace(code) == "" {
		return nil, ErrStateInvalid
	}
	st, err := s.Store.TakeState(ctx, state)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrStateInvalid
		}
		return nil, err
	}
	if st.ProviderID != githubManifestProviderID {
		return nil, ErrStateInvalid
	}
	if st.LinkUser == "" || st.LinkUser != sessionUser {
		return nil, ErrStateUserMismatch
	}

	creds, err := s.convertGitHubManifest(ctx, code)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(creds.ClientID) == "" || strings.TrimSpace(creds.ClientSecret) == "" {
		return nil, fmt.Errorf("github manifest: conversion returned no client credentials")
	}

	existingID := ""
	if providers, lerr := s.Store.ListProviders(ctx, false); lerr == nil {
		for _, p := range providers {
			if p.Kind == KindGitHub && p.Host == "github.com" {
				existingID = p.ID
				break
			}
		}
	}

	return s.SaveProvider(ctx, Provider{
		ID:           existingID,
		Kind:         KindGitHub,
		Host:         "github.com",
		Label:        "GitHub",
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Enabled:      true,
	})
}

func (s *Service) convertGitHubManifest(ctx context.Context, code string) (*githubAppCredentials, error) {
	httpc := s.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 15 * time.Second}
	}
	convertURL := s.githubConvertURL(code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, convertURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("github manifest conversion: status %d", resp.StatusCode)
	}
	var creds githubAppCredentials
	if err := json.Unmarshal(body, &creds); err != nil {
		return nil, fmt.Errorf("github manifest conversion: %w", err)
	}
	return &creds, nil
}

func (s *Service) githubConvertURL(code string) string {
	if s.GitHubConvertURL != nil {
		return s.GitHubConvertURL(code)
	}
	return fmt.Sprintf(githubManifestConvertFmt, code)
}
