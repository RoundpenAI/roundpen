package proactive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/issue"
)

// IssueLister lists open-ish issues for a user.
type IssueLister interface {
	List(ctx context.Context, userID string, f issue.ListFilter) ([]issue.Issue, error)
}

// GmailSearcher searches the user's connected Gmail (read-only).
type GmailSearcher interface {
	SearchGmail(ctx context.Context, userID, q string, maxResults int) ([]GmailHit, error)
}

// GmailHit is a light unread-mail row.
type GmailHit struct {
	ID      string
	Subject string
	From    string
	Snippet string
}

// GitHubToken resolves a GitHub API token for the user (PAT or OAuth).
type GitHubToken interface {
	Token(ctx context.Context, userID string) (string, error)
}

// OnThisDayScanner looks for anniversary photos under directory grants.
// P0 may return nil findings; implement when grants + photo index are ready.
type OnThisDayScanner interface {
	Scan(ctx context.Context, userID string, now time.Time) ([]Finding, error)
}

func scanIssues(ctx context.Context, issues IssueLister, userID string, limit int) ([]Finding, error) {
	if issues == nil {
		return []Finding{{Source: SourceIssues, Skipped: "issue store not configured"}}, nil
	}
	if limit <= 0 {
		limit = 5
	}
	openStatuses := []string{
		issue.StatusDrafting, issue.StatusSpecced, issue.StatusPlanned, issue.StatusInProgress,
	}
	seen := map[string]struct{}{}
	var out []Finding
	for _, st := range openStatuses {
		rows, err := issues.List(ctx, userID, issue.ListFilter{Status: st, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, it := range rows {
			if _, ok := seen[it.Key]; ok {
				continue
			}
			seen[it.Key] = struct{}{}
			title := strings.TrimSpace(it.Title)
			if title == "" {
				title = it.Key
			}
			out = append(out, Finding{
				Source: SourceIssues,
				Title:  fmt.Sprintf("%s · %s", it.Key, title),
				Detail: it.Status,
				URL:    "/issues/" + it.Key,
			})
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func scanGmail(ctx context.Context, gmail GmailSearcher, userID string, limit int) ([]Finding, error) {
	if gmail == nil {
		return []Finding{{Source: SourceGmail, Skipped: "gmail connector not configured"}}, nil
	}
	if limit <= 0 {
		limit = 5
	}
	rows, err := gmail.SearchGmail(ctx, userID, "is:unread newer_than:7d", limit)
	if err != nil {
		// Not connected is a soft skip, not a hard failure.
		msg := err.Error()
		if strings.Contains(msg, "not connected") || strings.Contains(msg, "not configured") {
			return []Finding{{Source: SourceGmail, Skipped: msg}}, nil
		}
		return nil, err
	}
	out := make([]Finding, 0, len(rows))
	for _, r := range rows {
		title := strings.TrimSpace(r.Subject)
		if title == "" {
			title = "(无主题)"
		}
		detail := strings.TrimSpace(r.From)
		if sn := strings.TrimSpace(r.Snippet); sn != "" {
			if detail != "" {
				detail += " · "
			}
			detail += sn
		}
		out = append(out, Finding{
			Source: SourceGmail,
			Title:  title,
			Detail: detail,
		})
	}
	return out, nil
}

func scanGitHub(ctx context.Context, tokens GitHubToken, client *http.Client, userID string, limit int) ([]Finding, error) {
	if tokens == nil {
		return []Finding{{Source: SourceGitHub, Skipped: "github token resolver not configured"}}, nil
	}
	token, err := tokens.Token(ctx, userID)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return []Finding{{Source: SourceGitHub, Skipped: "no GitHub token — connect under Settings → Accounts"}}, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if limit <= 0 {
		limit = 5
	}
	queries := []string{
		"is:pr is:open author:@me",
		"is:pr is:open review-requested:@me",
	}
	seen := map[string]struct{}{}
	var out []Finding
	for _, q := range queries {
		rows, err := githubSearchPRs(ctx, client, token, q, limit)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if _, ok := seen[r.HTMLURL]; ok {
				continue
			}
			seen[r.HTMLURL] = struct{}{}
			out = append(out, Finding{
				Source: SourceGitHub,
				Title:  r.Title,
				Detail: r.Repo,
				URL:    r.HTMLURL,
			})
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

type ghPR struct {
	Title   string
	Repo    string
	HTMLURL string
}

func githubSearchPRs(ctx context.Context, client *http.Client, token, q string, limit int) ([]ghPR, error) {
	u := "https://api.github.com/search/issues?" + url.Values{
		"q":        {q},
		"per_page": {fmt.Sprintf("%d", limit)},
		"sort":     {"updated"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("github auth failed (%d)", resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github search HTTP %d", resp.StatusCode)
	}
	var body struct {
		Items []struct {
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			RepoURL string `json:"repository_url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	out := make([]ghPR, 0, len(body.Items))
	for _, it := range body.Items {
		repo := strings.TrimPrefix(it.RepoURL, "https://api.github.com/repos/")
		out = append(out, ghPR{Title: it.Title, Repo: repo, HTMLURL: it.HTMLURL})
	}
	return out, nil
}
