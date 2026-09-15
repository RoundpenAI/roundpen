package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Skill is a named, model-invocable workflow. Executing a skill injects its
// instruction text into the conversation; the model then follows those
// instructions with the ordinary tools (Bash, Read, ...). Skills themselves
// carry no runtime — what the sandbox can do is defined by the agent template
// (git/curl/python/node are preinstalled in code-agent).
type Skill struct {
	// Name is the identifier the model passes to the Skill tool.
	Name string
	// Description tells the model/catalog what the skill does.
	Description string
	// Prompt is the instruction text injected when the skill runs.
	Prompt string
	// Params marks that the skill accepts a free-form `args` value from the model.
	Params bool
	// AllowedTools is a hint (not an enforcement) listing tools the skill is
	// intended to use; it is surfaced in the injected instructions.
	AllowedTools []string
	// Source is "builtin" or "workspace"; only meaningful for resolved skills.
	Source string
}

const (
	// SkillSourceBuiltin marks skills compiled into the agent (code fallback).
	SkillSourceBuiltin = "builtin"
	// SkillSourceWorkspace marks skills stored in the user's workspace.
	SkillSourceWorkspace = "workspace"

	skillsDirRel      = ".roundpen/skills"
	maxSkillFileBytes = 256 << 10
	skillFetchTimeout = 15 * time.Second
)

var skillNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// DefaultSkills returns the built-in skill set every System Agent gets.
func DefaultSkills() []Skill {
	return []Skill{
		{
			Name:         "commit",
			Description:  "Review workspace changes and write a clear, conventional commit message.",
			AllowedTools: []string{"Bash", "Read", "Glob", "Grep"},
			Prompt: `Review the current git state in the Agent workspace:
1. Use Bash ` + "`git status --short`" + ` and ` + "`git diff`" + ` (staged + unstaged) to see what changed.
2. Summarize what the user is trying to accomplish from the conversation and the diff.
3. Create a concise commit message following the conventional commits style (feat/fix/refactor/docs/chore...).
4. Commit ONLY if the user explicitly asked to commit. Otherwise present the proposed message and ask whether to commit.`,
			Params: true,
		},
		{
			Name:         "review",
			Description:  "Review the latest workspace changes for bugs, security, and clarity, then report findings.",
			AllowedTools: []string{"Bash", "Read", "Glob", "Grep"},
			Prompt: `Review the most recent changes in the Agent workspace:
1. Use Bash ` + "`git diff HEAD`" + ` and ` + "`git status --short`" + ` to scope the review; if there is no git history, list the workspace files with Glob and pick the relevant ones.
2. Read the changed files. Look for bugs, security issues, broken assumptions, missing error handling, and unclear code.
3. Report concise, prioritized findings with file:line references. Do not edit files during the review.`,
		},
		{
			Name:         "fix",
			Description:  "Fix toolchain issues (build, lint, test failures) reported or found in the workspace.",
			AllowedTools: []string{"Bash", "Read", "Write", "Edit", "Glob", "Grep"},
			Prompt: `Fix toolchain issues in the Agent workspace:
1. Reproduce the problem first: run the build, linter, or tests the user mentioned.
2. Read the errors and trace them to the responsible files before editing.
3. Apply minimal fixes that address root causes, then re-run to confirm.
4. If the issue is environmental rather than code (e.g. missing dependency), fix the environment with Bash and explain what you did.`,
			Params: true,
		},
		{
			Name:         "summarize",
			Description:  "Summarize the current state of the workspace and the conversation.",
			AllowedTools: []string{"Read", "Glob"},
			Prompt: `Summarize the current workspace and conversation state:
1. Briefly walk the workspace layout (Glob top-level, read key files) to understand what the project is.
2. Combine what was already completed, what is in progress, and what remains.
3. Output a concise structured summary the user can resume from.`,
		},
	}
}

func builtinSkill(name string) (Skill, bool) {
	if !skillNameRe.MatchString(name) {
		return Skill{}, false
	}
	for _, s := range DefaultSkills() {
		if s.Name == name {
			s.Source = SkillSourceBuiltin
			return s, true
		}
	}
	return Skill{}, false
}

func skillRelPath(name string) string {
	return path.Join(skillsDirRel, name+".md")
}

func skillGuestDir() string {
	return path.Join(WorkspaceRoot, skillsDirRel)
}

func skillGuestPath(name string) string {
	return path.Join(WorkspaceRoot, skillRelPath(name))
}

// readWorkspaceSkill loads a skill file from /workspace/.roundpen/skills.
// found=false (no error) means the file is absent, so callers fall back to
// built-ins. Invalid names and malformed files surface as errors.
func readWorkspaceSkill(ctx context.Context, b *AgentBinder, actor Actor, name string) (Skill, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Skill{}, false, fmt.Errorf("skill name is required")
	}
	if !skillNameRe.MatchString(name) {
		return Skill{}, false, fmt.Errorf("invalid skill name %q: use lowercase letters, digits, '-' and '_'", name)
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return Skill{}, false, err
	}
	if b.Files == nil {
		return Skill{}, false, fmt.Errorf("workspace files not configured")
	}
	rc, err := b.Files.ReadFile(ctx, id, skillRelPath(name))
	if err != nil {
		return Skill{}, false, nil // absent → built-in fallback
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, maxSkillFileBytes+1))
	if err != nil {
		return Skill{}, false, err
	}
	if len(raw) > maxSkillFileBytes {
		return Skill{}, false, fmt.Errorf("skill %q exceeds %d bytes", name, maxSkillFileBytes)
	}
	s, _, err := parseSkillFile(raw)
	if err != nil {
		return Skill{}, false, fmt.Errorf("skill %q: %w", name, err)
	}
	s.Name = name
	s.Source = SkillSourceWorkspace
	return s, true, nil
}

func listWorkspaceSkillNames(ctx context.Context, b *AgentBinder, actor Actor) ([]string, error) {
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return nil, err
	}
	if b.Exec == nil {
		return nil, fmt.Errorf("workspace exec not configured")
	}
	// basename "$f" .md strips the trailing ".md". The dir may not exist yet,
	// so guard with -d. Empty dirs produce no output.
	script := `D="$1"; if [ -d "$D" ]; then for f in "$D"/*.md; do [ -f "$f" ] || continue; basename "$f" .md; done; fi`
	res, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", script, "skill-list", skillGuestDir()}, WorkspaceRoot, defaultExecTimeout)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if !skillNameRe.MatchString(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// parseSkillFile parses a SKILL.md file: optional YAML-ish frontmatter
// (--- delimited) holding name/description/args/allowedTools, followed by the
// instruction body. Unknown keys are ignored; the parser is intentionally
// minimal and needs no YAML dependency.
func parseSkillFile(raw []byte) (Skill, string, error) {
	text := strings.TrimPrefix(string(raw), "\ufeff")
	var s Skill
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		var fm, body []string
		if end < 0 {
			fm = lines[1:]
		} else {
			fm = lines[1:end]
			body = lines[end+1:]
		}
		s = parseSkillFrontmatter(fm)
		if end < 0 {
			body = nil
		}
		text = strings.Join(body, "\n")
	}
	body := strings.TrimSpace(text)
	if body == "" {
		return Skill{}, "", fmt.Errorf("skill body is empty (expected instructions after the frontmatter)")
	}
	s.Prompt = body
	return s, body, nil
}

func parseSkillFrontmatter(lines []string) Skill {
	var s Skill
	inTools := false
	for _, ln := range lines {
		line := strings.TrimSpace(ln)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			if inTools {
				s.AllowedTools = append(s.AllowedTools, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
			}
			continue
		}
		inTools = false
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		switch key {
		case "name":
			s.Name = val
		case "description":
			s.Description = val
		case "args", "params":
			s.Params = (val == "true")
		case "allowedTools":
			s.AllowedTools = nil
			inTools = true
		}
	}
	return s
}

// marshalSkillFile serializes a skill back to the SKILL.md format, so every
// installed skill round-trips through the same canonical layout.
func marshalSkillFile(s Skill) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", s.Name)
	fmt.Fprintf(&b, "description: %s\n", s.Description)
	fmt.Fprintf(&b, "args: %t\n", s.Params)
	if len(s.AllowedTools) > 0 {
		b.WriteString("allowedTools:\n")
		for _, t := range s.AllowedTools {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(t))
		}
	}
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(s.Prompt))
	b.WriteString("\n")
	return b.String()
}

// RegisterSkill adds the Skill tool used to invoke named skills. Binder runs
// workspace storage at /workspace/.roundpen/skills; web (SSRF-protected) is
// used for install-from-URL and may be nil to disable that path.
func RegisterSkill(r *Registry, binder *AgentBinder, web *http.Client) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "Skill",
		Description: "Run a curated workflow from its instructions. Actions: " +
			"invoke (default) runs a skill by name and returns instructions to follow; " +
			"list shows available skills; " +
			"install adds a skill from a URL or inline content (overwriting an existing one asks the user first); " +
			"remove deletes an installed skill. " +
			"Skills only affect how you work - they do not unlock new capabilities. " +
			"Invocations are capped at 3 per reply; beyond that, ask the user to continue in a new message.",
		Parameters: objectSchema(map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []any{"invoke", "list", "install", "remove"},
				"description": "What to do (default invoke)",
			},
			"skill":   map[string]any{"type": "string", "description": "Skill name"},
			"args":    map[string]any{"type": "string", "description": "Free-form arguments for the skill (invoke only)"},
			"url":     map[string]any{"type": "string", "description": "Download the skill from this URL (install only; requires a skill name or a name: in the file)"},
			"content": map[string]any{"type": "string", "description": "Inline SKILL.md content (install only)"},
		}),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Action  string `json:"action"`
				Skill   string `json:"skill"`
				Args    string `json:"args"`
				URL     string `json:"url"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			switch action := strings.TrimSpace(in.Action); action {
			case "", "invoke":
				return skillInvoke(ctx, binder, actor, in.Skill, in.Args)
			case "list":
				return skillList(ctx, binder, actor)
			case "install":
				return skillInstall(ctx, binder, web, actor, in.URL, in.Content, in.Skill)
			case "remove":
				return skillRemove(ctx, binder, actor, in.Skill)
			default:
				return "", fmt.Errorf("unknown action %q; valid actions: invoke, list, install, remove", action)
			}
		},
	})
}

func skillInvoke(ctx context.Context, b *AgentBinder, actor Actor, name, args string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	s, found, err := readWorkspaceSkill(ctx, b, actor, name)
	if err != nil {
		return "", err
	}
	if !found {
		if builtin, ok := builtinSkill(name); ok {
			s, found = builtin, true
		}
	}
	if !found {
		return "", fmt.Errorf("unknown skill %q; use Skill with action \"list\" to see available skills", name)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Running skill %q (%s).\n\n%s", s.Name, s.Source, strings.TrimSpace(s.Prompt))
	if s.Params && strings.TrimSpace(args) != "" {
		fmt.Fprintf(&sb, "\n\nSkill arguments:\n%s", strings.TrimSpace(args))
	}
	if len(s.AllowedTools) > 0 {
		fmt.Fprintf(&sb, "\n\nThis skill is intended to use these tools: %s", strings.Join(s.AllowedTools, ", "))
	}
	return sb.String(), nil
}

func skillList(ctx context.Context, b *AgentBinder, actor Actor) (string, error) {
	ws, err := listWorkspaceSkillNames(ctx, b, actor)
	if err != nil {
		return "", err
	}
	wsSet := make(map[string]bool, len(ws))
	for _, n := range ws {
		wsSet[n] = true
	}
	names := make(map[string]bool)
	for _, s := range DefaultSkills() {
		names[s.Name] = true
	}
	for _, n := range ws {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Available skills (%d):\n", len(sorted))
	for _, n := range sorted {
		source := SkillSourceBuiltin
		desc := ""
		if wsSet[n] {
			source = SkillSourceWorkspace
			if s, ok, err := readWorkspaceSkill(ctx, b, actor, n); err == nil && ok {
				desc = s.Description
			} else {
				desc = "workspace skill"
			}
		} else if s, ok := builtinSkill(n); ok {
			desc = s.Description
		}
		fmt.Fprintf(&sb, "- %-16s %s [%s]\n", n, desc, source)
	}
	return sb.String(), nil
}

func skillInstall(ctx context.Context, b *AgentBinder, web *http.Client, actor Actor, rawURL, content, nameArg string) (string, error) {
	srcURL, content := strings.TrimSpace(rawURL), strings.TrimSpace(content)
	haveURL, haveContent := srcURL != "", content != ""
	switch {
	case haveURL && haveContent:
		return "", fmt.Errorf("provide either url or content, not both")
	case haveURL:
	case haveContent:
	default:
		return "", fmt.Errorf("install requires either a url or inline content")
	}
	if haveURL {
		if web == nil {
			return "", fmt.Errorf("skill install from URL is not supported in this deployment")
		}
		fetched, err := fetchSkillURL(ctx, web, srcURL)
		if err != nil {
			return "", err
		}
		content = fetched
	}

	s, _, err := parseSkillFile([]byte(content))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s.Description) == "" {
		return "", fmt.Errorf("skill is missing a description in its frontmatter")
	}
	if strings.Contains(s.Description, "\n") {
		return "", fmt.Errorf("skill description must be a single line")
	}

	name := strings.TrimSpace(nameArg)
	switch {
	case s.Name != "" && name != "" && s.Name != name:
		return "", fmt.Errorf("frontmatter name %q conflicts with the skill argument %q", s.Name, name)
	case s.Name != "":
		name = s.Name
	case name == "":
		name = skillNameFromURL(srcURL)
	}
	if name == "" {
		return "", fmt.Errorf("could not determine the skill name; pass the skill argument or a name: in the file")
	}
	if !skillNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid skill name %q: use lowercase letters, digits, '-' and '_'", name)
	}
	s.Name = name

	if _, found, err := readWorkspaceSkill(ctx, b, actor, name); err != nil {
		return "", err
	} else if found {
		ok, err := confirmAction(ctx, fmt.Sprintf("A workspace skill named %q already exists. Overwrite it?", name), "Overwrite")
		if err != nil {
			return "", err
		}
		if !ok {
			return fmt.Sprintf("Skill %q not installed: kept the existing workspace skill.", name), nil
		}
	} else if _, builtin := builtinSkill(name); builtin {
		ok, err := confirmAction(ctx, fmt.Sprintf("A built-in skill named %q already exists and installing this one will shadow it. Overwrite?", name), "Overwrite")
		if err != nil {
			return "", err
		}
		if !ok {
			return fmt.Sprintf("Skill %q not installed: the built-in skill is kept.", name), nil
		}
	}

	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	if b.Files == nil {
		return "", fmt.Errorf("workspace files not configured")
	}
	if err := b.Files.WriteFile(ctx, id, skillRelPath(name), strings.NewReader(marshalSkillFile(s))); err != nil {
		return "", fmt.Errorf("write skill: %w", err)
	}
	return fmt.Sprintf("Installed skill %q to %s. Use Skill with action \"invoke\" to run it.", name, skillGuestPath(name)), nil
}

func skillRemove(ctx context.Context, b *AgentBinder, actor Actor, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	if _, found, err := readWorkspaceSkill(ctx, b, actor, name); err != nil {
		return "", err
	} else if !found {
		if _, builtin := builtinSkill(name); builtin {
			return "", fmt.Errorf("built-in skill %q cannot be removed; only a workspace skill that shadows it can be deleted", name)
		}
		return "", fmt.Errorf("no skill %q is installed in the workspace", name)
	}
	ok, err := confirmAction(ctx, fmt.Sprintf("Remove skill %q from the workspace?", name), "Remove")
	if err != nil {
		return "", err
	}
	if !ok {
		return fmt.Sprintf("Skill %q not removed.", name), nil
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	if b.Exec == nil {
		return "", fmt.Errorf("workspace exec not configured")
	}
	script := `F="$1"; if [ -f "$F" ]; then rm -f -- "$F"; fi`
	if _, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", script, "skill-rm", skillGuestPath(name)}, WorkspaceRoot, defaultExecTimeout); err != nil {
		return "", err
	}
	return fmt.Sprintf("Removed skill %q.", name), nil
}

// confirmAction asks the user a yes/no question through the session
// interactor. Returns (false, nil) when the user picks Cancel.
func confirmAction(ctx context.Context, question, yesLabel string) (bool, error) {
	si := SessionInteractor(ctx)
	if si == nil || si.AskUser == nil {
		return false, fmt.Errorf("interactive session not available")
	}
	answer, err := si.AskUser(ctx, AskQuestion{
		Question: question,
		Header:   "Confirm",
		Options: []AnswerOption{
			{Label: yesLabel},
			{Label: "Cancel"},
		},
	})
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(answer), yesLabel), nil
}

func fetchSkillURL(ctx context.Context, web *http.Client, rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q; only http/https", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("url is missing a host")
	}
	ctx, cancel := context.WithTimeout(ctx, skillFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "roundpen-skill-installer/1.0")
	resp, err := web.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch skill: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch skill: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSkillFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxSkillFileBytes {
		return "", fmt.Errorf("skill file exceeds %d bytes", maxSkillFileBytes)
	}
	return string(raw), nil
}

// skillNameFromURL derives a skill name from the URL's file basename
// (e.g. ".../review-pr.md" → "review-pr").
func skillNameFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Path == "" {
		return ""
	}
	base := strings.TrimSpace(path.Base(u.Path))
	for _, ext := range []string{".md", ".markdown", ".skill"} {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	if !skillNameRe.MatchString(base) {
		return ""
	}
	return base
}
