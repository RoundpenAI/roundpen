package project

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GitHubEvent is a minimal parsed webhook payload.
type GitHubEvent struct {
	Event    string // X-GitHub-Event
	Action   string
	Repo     string // owner/name
	Summary  string
	Status   string // success | failure | pending | cancelled | "" for non-CI
	Relevant bool
}

// ParseGitHubWebhook maps delivery headers + body into a summary for Roundpen.
func ParseGitHubWebhook(eventType string, body []byte) GitHubEvent {
	eventType = strings.TrimSpace(eventType)
	out := GitHubEvent{Event: eventType}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		out.Summary = "unparseable webhook body"
		return out
	}
	out.Action = jsonString(raw["action"])
	out.Repo = repoFullName(raw["repository"])

	switch eventType {
	case "pull_request":
		out.Relevant = true
		pr := jsonObject(raw["pull_request"])
		num := jsonInt(pr["number"])
		title := jsonString(pr["title"])
		state := jsonString(pr["state"])
		url := jsonString(pr["html_url"])
		out.Summary = fmt.Sprintf("PR #%d %s — %s (%s)\n%s", num, out.Action, title, state, url)
	case "pull_request_review":
		out.Relevant = true
		pr := jsonObject(raw["pull_request"])
		rev := jsonObject(raw["review"])
		num := jsonInt(pr["number"])
		state := jsonString(rev["state"])
		url := jsonString(rev["html_url"])
		out.Summary = fmt.Sprintf("PR #%d review %s — %s\n%s", num, out.Action, state, url)
	case "check_run":
		out.Relevant = true
		cr := jsonObject(raw["check_run"])
		name := jsonString(cr["name"])
		concl := jsonString(cr["conclusion"])
		status := jsonString(cr["status"])
		url := jsonString(cr["html_url"])
		out.Status = mapCIStatus(status, concl)
		out.Summary = fmt.Sprintf("Check %q %s (conclusion=%s)\n%s", name, status, concl, url)
	case "check_suite":
		out.Relevant = true
		cs := jsonObject(raw["check_suite"])
		concl := jsonString(cs["conclusion"])
		status := jsonString(cs["status"])
		out.Status = mapCIStatus(status, concl)
		out.Summary = fmt.Sprintf("Check suite %s (conclusion=%s)", status, concl)
	case "workflow_run":
		out.Relevant = true
		wr := jsonObject(raw["workflow_run"])
		name := jsonString(wr["name"])
		concl := jsonString(wr["conclusion"])
		status := jsonString(wr["status"])
		url := jsonString(wr["html_url"])
		out.Status = mapCIStatus(status, concl)
		out.Summary = fmt.Sprintf("Workflow %q %s (conclusion=%s)\n%s", name, status, concl, url)
	case "status":
		out.Relevant = true
		state := jsonString(raw["state"])
		ctx := jsonString(raw["context"])
		desc := jsonString(raw["description"])
		url := jsonString(raw["target_url"])
		out.Status = mapLegacyStatus(state)
		out.Summary = fmt.Sprintf("Status %q → %s — %s\n%s", ctx, state, desc, url)
	default:
		out.Summary = fmt.Sprintf("GitHub event %q action=%s", eventType, out.Action)
	}
	out.Summary = strings.TrimSpace(out.Summary)
	return out
}

// EventMatchesKind reports whether the GitHub event belongs to a subscription kind.
func EventMatchesKind(kind, eventType string) bool {
	switch kind {
	case SubKindGitHubPR:
		return eventType == "pull_request" || eventType == "pull_request_review"
	case SubKindGitHubCI:
		switch eventType {
		case "check_run", "check_suite", "workflow_run", "status":
			return true
		}
	}
	return false
}

func mapCIStatus(status, conclusion string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	conclusion = strings.ToLower(strings.TrimSpace(conclusion))
	if status == "queued" || status == "in_progress" || status == "pending" {
		return "pending"
	}
	switch conclusion {
	case "success", "neutral", "skipped":
		return "success"
	case "failure", "timed_out", "action_required", "startup_failure":
		return "failure"
	case "cancelled":
		return "cancelled"
	case "":
		if status == "completed" {
			return "success"
		}
		return "pending"
	default:
		return conclusion
	}
}

func mapLegacyStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "success":
		return "success"
	case "pending":
		return "pending"
	case "failure", "error":
		return "failure"
	default:
		return strings.ToLower(strings.TrimSpace(state))
	}
}

func repoFullName(raw json.RawMessage) string {
	obj := jsonObject(raw)
	if full := jsonString(obj["full_name"]); full != "" {
		return full
	}
	owner := jsonObject(obj["owner"])
	login := jsonString(owner["login"])
	name := jsonString(obj["name"])
	if login != "" && name != "" {
		return login + "/" + name
	}
	return ""
}

func jsonObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	return m
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func jsonInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	return 0
}
