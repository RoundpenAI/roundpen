package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/vcs"
)

const gitPRTimeout = 3 * time.Minute

// RegisterGitPR adds first-class commit / push / PR tools (GitHub via `gh` in the
// agent sandbox). Gitea/GitLab kinds are detected and rejected with a clear
// extension-point message until backends land.
func RegisterGitPR(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "GitCommit",
		Description: "Stage paths and create a git commit in a repo under /workspace. " +
			"Does not push. Prefer conventional commit messages.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"repo_path": map[string]any{"type": "string", "description": "Repo root under /workspace (e.g. projects/foo)"},
			"message":   map[string]any{"type": "string", "description": "Commit message"},
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional paths to stage; omit to stage all tracked/untracked changes (-A)",
			},
		}, "repo_path", "message"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				RepoPath string   `json:"repo_path"`
				Message  string   `json:"message"`
				Paths    []string `json:"paths"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid args")
			}
			return binder.gitCommit(ctx, actor, in.RepoPath, in.Message, in.Paths)
		},
	})
	r.Register(Tool{
		Name: "GitPush",
		Description: "Push the current branch to its remote (git push -u origin HEAD). " +
			"Never force-pushes. Roundpen always opens a PR afterward — do not push directly to the default branch as the delivery path.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"repo_path": map[string]any{"type": "string", "description": "Repo root under /workspace"},
			"remote":    map[string]any{"type": "string", "description": "Remote name (default origin)"},
			"set_upstream": map[string]any{
				"type":        "boolean",
				"description": "Pass -u (default true)",
			},
		}, "repo_path"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				RepoPath    string `json:"repo_path"`
				Remote      string `json:"remote"`
				SetUpstream *bool  `json:"set_upstream"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid args")
			}
			up := true
			if in.SetUpstream != nil {
				up = *in.SetUpstream
			}
			return binder.gitPush(ctx, actor, in.RepoPath, in.Remote, up)
		},
	})
	r.Register(Tool{
		Name: "CreatePullRequest",
		Description: "Open a GitHub pull request with gh (always create a PR; do not merge to default without a PR). " +
			"Requires GitHub auth in the agent sandbox (Settings → Git / OAuth). Gitea/GitLab: not implemented yet.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"repo_path": map[string]any{"type": "string", "description": "Repo root under /workspace"},
			"title":     map[string]any{"type": "string"},
			"body":      map[string]any{"type": "string", "description": "PR body markdown"},
			"base":      map[string]any{"type": "string", "description": "Base branch (default: remote default)"},
			"draft":     map[string]any{"type": "boolean"},
		}, "repo_path", "title"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				RepoPath string `json:"repo_path"`
				Title    string `json:"title"`
				Body     string `json:"body"`
				Base     string `json:"base"`
				Draft    bool   `json:"draft"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid args")
			}
			return binder.createPullRequest(ctx, actor, in.RepoPath, vcs.CreatePRInput{
				Title: in.Title, Body: in.Body, Base: in.Base, Draft: in.Draft,
			})
		},
	})
	r.Register(Tool{
		Name: "MergePullRequest",
		Description: "Merge a GitHub pull request with gh (default squash). Always asks the user (SoftDeny). " +
			"Never force-pushes. Gitea/GitLab: not implemented yet.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"repo_path": map[string]any{"type": "string", "description": "Repo root under /workspace"},
			"number":    map[string]any{"type": "integer", "description": "PR number"},
			"method": map[string]any{
				"type":        "string",
				"description": "squash (default) | merge | rebase",
				"enum":        []any{"squash", "merge", "rebase"},
			},
		}, "repo_path", "number"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				RepoPath string `json:"repo_path"`
				Number   int    `json:"number"`
				Method   string `json:"method"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid args")
			}
			return binder.mergePullRequest(ctx, actor, in.RepoPath, vcs.MergePRInput{
				Number: in.Number, Method: in.Method,
			})
		},
	})
}

func (b *AgentBinder) gitCommit(ctx context.Context, actor Actor, repoPath, message string, paths []string) (string, error) {
	wd, err := resolveRepoWorkdir(repoPath)
	if err != nil {
		return "", err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("message is required")
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	stage := []string{"git", "add"}
	if len(paths) == 0 {
		stage = append(stage, "-A")
	} else {
		for _, p := range paths {
			p = strings.TrimSpace(p)
			if p == "" || strings.Contains(p, "..") {
				return "", fmt.Errorf("invalid path")
			}
			stage = append(stage, p)
		}
	}
	if out, err := b.exec(ctx, id, stage, wd, gitPRTimeout); err != nil {
		return out, fmt.Errorf("git add failed: %w", err)
	}
	out, err := b.exec(ctx, id, []string{"git", "commit", "-m", message}, wd, gitPRTimeout)
	if err != nil {
		return out, fmt.Errorf("git commit failed: %w", err)
	}
	return out, nil
}

func (b *AgentBinder) gitPush(ctx context.Context, actor Actor, repoPath, remote string, setUpstream bool) (string, error) {
	wd, err := resolveRepoWorkdir(repoPath)
	if err != nil {
		return "", err
	}
	if remote == "" {
		remote = "origin"
	}
	if strings.ContainsAny(remote, " \t\n\"'") {
		return "", fmt.Errorf("invalid remote")
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	// Refuse if current branch is the remote default — delivery path is always PR.
	defOut, _ := b.exec(ctx, id, []string{"git", "symbolic-ref", "refs/remotes/" + remote + "/HEAD"}, wd, 30*time.Second)
	branchOut, _ := b.exec(ctx, id, []string{"git", "rev-parse", "--abbrev-ref", "HEAD"}, wd, 30*time.Second)
	branch := strings.TrimSpace(extractStdout(branchOut))
	defRef := strings.TrimSpace(extractStdout(defOut))
	if branch != "" && defRef != "" {
		defBranch := path.Base(defRef)
		if branch == defBranch {
			return "", fmt.Errorf("refusing to push default branch %q — create a feature branch and open a PR", branch)
		}
	}
	cmd := []string{"git", "push"}
	if setUpstream {
		cmd = append(cmd, "-u")
	}
	cmd = append(cmd, remote, "HEAD")
	out, err := b.exec(ctx, id, cmd, wd, gitPRTimeout)
	if err != nil {
		return out, fmt.Errorf("git push failed: %w", err)
	}
	return out, nil
}

func (b *AgentBinder) createPullRequest(ctx context.Context, actor Actor, repoPath string, in vcs.CreatePRInput) (string, error) {
	wd, err := resolveRepoWorkdir(repoPath)
	if err != nil {
		return "", err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return "", fmt.Errorf("title is required")
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	if err := b.requireGitHubCLI(ctx, id, wd); err != nil {
		return "", err
	}
	cmd := []string{"gh", "pr", "create", "--title", in.Title}
	if strings.TrimSpace(in.Body) != "" {
		cmd = append(cmd, "--body", in.Body)
	} else {
		cmd = append(cmd, "--body", "")
	}
	if strings.TrimSpace(in.Base) != "" {
		cmd = append(cmd, "--base", in.Base)
	}
	if in.Draft {
		cmd = append(cmd, "--draft")
	}
	out, err := b.exec(ctx, id, cmd, wd, gitPRTimeout)
	if err != nil {
		return out, fmt.Errorf("gh pr create failed: %w", err)
	}
	return out, nil
}

func (b *AgentBinder) mergePullRequest(ctx context.Context, actor Actor, repoPath string, in vcs.MergePRInput) (string, error) {
	wd, err := resolveRepoWorkdir(repoPath)
	if err != nil {
		return "", err
	}
	if in.Number <= 0 {
		return "", fmt.Errorf("number is required")
	}
	method := strings.ToLower(strings.TrimSpace(in.Method))
	if method == "" {
		method = "squash"
	}
	switch method {
	case "squash", "merge", "rebase":
	default:
		return "", fmt.Errorf("method must be squash, merge, or rebase")
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	if err := b.requireGitHubCLI(ctx, id, wd); err != nil {
		return "", err
	}
	flag := "--squash"
	switch method {
	case "merge":
		flag = "--merge"
	case "rebase":
		flag = "--rebase"
	}
	out, err := b.exec(ctx, id, []string{
		"gh", "pr", "merge", fmt.Sprintf("%d", in.Number), flag, "--delete-branch=false",
	}, wd, gitPRTimeout)
	if err != nil {
		return out, fmt.Errorf("gh pr merge failed: %w", err)
	}
	return out, nil
}

func (b *AgentBinder) requireGitHubCLI(ctx context.Context, id, wd string) error {
	remoteOut, err := b.exec(ctx, id, []string{"git", "remote", "get-url", "origin"}, wd, 30*time.Second)
	if err != nil {
		return fmt.Errorf("need git remote origin: %w", err)
	}
	remote := strings.TrimSpace(extractStdout(remoteOut))
	kind := vcs.DetectKind(remote)
	if kind != vcs.KindGitHub {
		return fmt.Errorf("%s remotes are not supported yet for CreatePullRequest/MergePullRequest (GitHub only in P1; %s is an extension point)", kind, kind)
	}
	if _, err := b.exec(ctx, id, []string{"gh", "auth", "status"}, wd, 30*time.Second); err != nil {
		// gh may still work via GH_TOKEN without auth status succeeding; try version.
		if _, verErr := b.exec(ctx, id, []string{"gh", "version"}, wd, 15*time.Second); verErr != nil {
			return fmt.Errorf("gh CLI not available in agent sandbox — install gh or authorize GitHub under Settings")
		}
	}
	return nil
}

func resolveRepoWorkdir(repoPath string) (string, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return "", fmt.Errorf("repo_path is required")
	}
	rel, err := ResolveWorkspacePath(repoPath)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return WorkspaceRoot, nil
	}
	return path.Join(WorkspaceRoot, rel), nil
}

func extractStdout(execJSON string) string {
	var payload struct {
		Stdout string `json:"stdout"`
	}
	if err := json.Unmarshal([]byte(execJSON), &payload); err != nil {
		return strings.TrimSpace(execJSON)
	}
	return strings.TrimSpace(payload.Stdout)
}
