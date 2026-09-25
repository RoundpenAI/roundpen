package tools

import (
	"context"
	"encoding/base64"
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
	// Source is "builtin" or "installed"; only meaningful for resolved skills.
	Source string
}

const (
	// SkillSourceBuiltin marks skills compiled into the agent (code fallback).
	SkillSourceBuiltin = "builtin"
	// SkillSourceInstalled marks skills stored in the agent's home
	// (~/.roundpen/skills), private to the user and outside the project workspace.
	SkillSourceInstalled = "installed"

	skillsHomeRel     = ".roundpen/skills"
	maxSkillFileBytes = 256 << 10
	skillFetchTimeout = 15 * time.Second
	skillListTimeout  = 5 * time.Second
)

// Shell snippets used to manage installed skills. Everything lives under the
// agent's $HOME, never inside /workspace: skills are private configuration and
// must not become part of the project the model reads/edits. Skill names are
// validated with skillNameRe before any interpolation, so "$1"/"$2" stay safe.
const (
	readSkillScript   = `F="$HOME/.roundpen/skills/$1"; if [ -f "$F" ]; then cat -- "$F"; fi`
	listSkillsScript  = `D="$HOME/.roundpen/skills"; if [ -d "$D" ]; then for f in "$D"/*.md; do [ -f "$f" ] || continue; basename "$f" .md; done; fi`
	writeSkillScript  = `D="$HOME/.roundpen/skills"; mkdir -p "$D"; printf %s "$1" | base64 -d > "$D/$2"`
	removeSkillScript = `F="$HOME/.roundpen/skills/$1"; if [ -f "$F" ]; then rm -f -- "$F"; fi`
	homeDisplayScript = `printf %s "$HOME"`
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
	return path.Join(skillsHomeRel, name+".md")
}

func skillGuestPath(home, name string) string {
	return path.Join(home, skillRelPath(name))
}

// readInstalledSkill loads a skill file from ~/.roundpen/skills inside the
// agent container. found=false (no error) means the file is absent, so callers
// fall back to built-ins. Invalid names and malformed files surface as errors.
func readInstalledSkill(ctx context.Context, b *AgentBinder, actor Actor, name string) (Skill, bool, error) {
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
	res, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", readSkillScript, "skill-read", name}, WorkspaceRoot, defaultExecTimeout)
	if err != nil {
		return Skill{}, false, err
	}
	if len(res.Stdout) > maxSkillFileBytes {
		return Skill{}, false, fmt.Errorf("skill %q exceeds %d bytes", name, maxSkillFileBytes)
	}
	if len(strings.TrimSpace(string(res.Stdout))) == 0 {
		return Skill{}, false, nil // absent → built-in fallback
	}
	s, _, err := parseSkillFile(res.Stdout)
	if err != nil {
		return Skill{}, false, fmt.Errorf("skill %q: %w", name, err)
	}
	s.Name = name
	s.Source = SkillSourceInstalled
	return s, true, nil
}

func listInstalledSkillNames(ctx context.Context, b *AgentBinder, actor Actor) ([]string, error) {
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return nil, err
	}
	res, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", listSkillsScript, "skill-list"}, WorkspaceRoot, defaultExecTimeout)
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

// skillHomeDisplay resolves the agent's HOME so install/remove messages can
// print a concrete path. Falls back to the "$HOME" placeholder on failure.
func skillHomeDisplay(ctx context.Context, b *AgentBinder, actor Actor) string {
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "$HOME"
	}
	res, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", homeDisplayScript, "skill-home"}, WorkspaceRoot, defaultExecTimeout)
	if err != nil || res.ExitCode != 0 {
		return "$HOME"
	}
	h := strings.TrimSpace(string(res.Stdout))
	if h == "" {
		return "$HOME"
	}
	return h
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
	consumed := -1
	for i := 0; i < len(lines); i++ {
		if i <= consumed {
			continue
		}
		if inTools {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "- ") {
				s.AllowedTools = append(s.AllowedTools, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
				continue
			}
			if trimmed == "" {
				continue
			}
			inTools = false
		}
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "- ") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := unquote(strings.TrimSpace(line[idx+1:]))
		switch key {
		case "name":
			s.Name = val
		case "description":
			switch {
			case isLiteralBlock(val):
				desc, end := collectBlockText(lines, i)
				s.Description = desc
				consumed = end
			case isFoldedBlock(val):
				desc, end := collectBlockText(lines, i)
				// YAML ">" folds the block into one logical line.
				s.Description = strings.Join(strings.Fields(desc), " ")
				consumed = end
			default:
				s.Description = val
			}
		case "args", "params":
			s.Params = (val == "true")
		case "allowedTools":
			s.AllowedTools = nil
			inTools = true
		}
	}
	return s
}

func unquote(val string) string {
	v := strings.TrimSpace(val)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return val
}

// isLiteralBlock reports whether val is a YAML literal scalar indicator ("|"/"|-").
func isLiteralBlock(val string) bool {
	return val == "|" || val == "|-"
}

// isFoldedBlock reports whether val is a YAML folded scalar indicator (">"/">-").
func isFoldedBlock(val string) bool {
	return val == ">" || val == ">-"
}

// collectBlockText gathers the indented lines following a block scalar
// indicator at lines[keyLine] and returns the text plus the index of the last
// consumed line. Content indentation is the indent of the first block line.
func collectBlockText(lines []string, keyLine int) (string, int) {
	var out []string
	i := keyLine + 1
	indent := -1
	for ; i < len(lines); i++ {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" {
			out = append(out, "")
			continue
		}
		lead := len(ln) - len(strings.TrimLeft(ln, " \t"))
		if lead == 0 {
			break
		}
		if indent < 0 {
			indent = lead
		}
		if lead >= indent {
			out = append(out, ln[indent:])
		} else {
			out = append(out, strings.TrimSpace(ln))
		}
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n"), i - 1
}

// marshalSkillFile serializes a skill back to the SKILL.md format, so every
// installed skill round-trips through the same canonical layout.
func marshalSkillFile(s Skill) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", s.Name)
	writeDescription(&b, s.Description)
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

// writeDescription emits a YAML scalar: inline for single-line values, a
// literal block ("|") when the description spans multiple lines.
func writeDescription(b *strings.Builder, desc string) {
	if strings.Contains(desc, "\n") {
		b.WriteString("description: |\n")
		for _, l := range strings.Split(desc, "\n") {
			fmt.Fprintf(b, "  %s\n", l)
		}
		return
	}
	fmt.Fprintf(b, "description: %s\n", desc)
}

// RegisterSkill adds the Skill tool used to invoke named skills. Binder stores
// installed skills in the agent's home (~/.roundpen/skills/<name>.md), outside
// the /workspace project; web (SSRF-protected) is used for install-from-URL and
// may be nil to disable that path.
func RegisterSkill(r *Registry, binder *AgentBinder, web *http.Client) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "Skill",
		// Mutating as a whole (install/remove write the workspace); the agent's
		// dispatch gate refines this per action: invoke and list are read-only.
		Mutating: true,
		Description: "Run a curated workflow from its instructions. Actions: " +
			"invoke (default) runs a skill by name and returns instructions to follow; " +
			"list shows available skills; " +
			"install adds a skill from a URL or inline content (overwriting an existing one asks the user first); " +
			"remove deletes an installed skill. " +
			"Each skill is a single markdown file at ~/.roundpen/skills/<name>.md in the agent's home - " +
			"private to the user and never part of the /workspace project. " +
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
	return ExpandInstalledSkill(ctx, b, actor, name, args)
}

// ExpandSkill resolves a built-in skill by name and returns the instruction
// text a caller injects. It never touches the sandbox, so user-typed slash
// commands work even when no agent environment is running.
func ExpandSkill(name, args string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	s, ok := builtinSkill(name)
	if !ok {
		return "", fmt.Errorf("unknown skill %q; use Skill with action \"list\" to see available skills", name)
	}
	return formatSkillInvocation(s, args), nil
}

// ExpandInstalledSkill is the full resolution for callers with a binder:
// installed skills in the agent home shadow built-ins, exactly like the Skill
// tool's invoke action. Slash commands and the tool share this path so both
// inject byte-identical instructions.
func ExpandInstalledSkill(ctx context.Context, b *AgentBinder, actor Actor, name, args string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	s, found, err := readInstalledSkill(ctx, b, actor, name)
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
	return formatSkillInvocation(s, args), nil
}

// formatSkillInvocation renders the text both invocation paths inject.
func formatSkillInvocation(s Skill, args string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Running skill %q (%s).\n\n%s", s.Name, s.Source, strings.TrimSpace(s.Prompt))
	if s.Params && strings.TrimSpace(args) != "" {
		fmt.Fprintf(&sb, "\n\nSkill arguments:\n%s", strings.TrimSpace(args))
	}
	if len(s.AllowedTools) > 0 {
		fmt.Fprintf(&sb, "\n\nThis skill is intended to use these tools: %s", strings.Join(s.AllowedTools, ", "))
	}
	return sb.String()
}

// ListInstalledSkills returns the skills installed in the agent home, with
// descriptions for catalog display. A skill whose file cannot be parsed is
// reported as "unreadable" instead of failing the whole listing; only the
// sandbox exec itself surfaces an error.
func ListInstalledSkills(ctx context.Context, b *AgentBinder, actor Actor) ([]Skill, error) {
	ctx, cancel := context.WithTimeout(ctx, skillListTimeout)
	defer cancel()
	names, err := listInstalledSkillNames(ctx, b, actor)
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(names))
	for _, name := range names {
		s, found, err := readInstalledSkill(ctx, b, actor, name)
		switch {
		case err != nil:
			out = append(out, Skill{
				Name:        name,
				Description: "installed skill (unreadable)",
				Source:      SkillSourceInstalled,
			})
		case found:
			out = append(out, s)
		}
	}
	return out, nil
}

func skillList(ctx context.Context, b *AgentBinder, actor Actor) (string, error) {
	inst, err := listInstalledSkillNames(ctx, b, actor)
	if err != nil {
		return "", err
	}
	instSet := make(map[string]bool, len(inst))
	for _, n := range inst {
		instSet[n] = true
	}
	names := make(map[string]bool)
	for _, s := range DefaultSkills() {
		names[s.Name] = true
	}
	for _, n := range inst {
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
		if instSet[n] {
			source = SkillSourceInstalled
			if s, ok, err := readInstalledSkill(ctx, b, actor, n); err == nil && ok {
				desc = s.Description
			} else if err != nil {
				desc = "installed skill (unreadable: " + err.Error() + ")"
			} else {
				desc = "installed skill"
			}
		} else if s, ok := builtinSkill(n); ok {
			desc = s.Description
		}
		// Keep the table one line even for multi-line descriptions.
		oneLine := strings.Join(strings.Fields(desc), " ")
		fmt.Fprintf(&sb, "- %-16s %s [%s]\n", n, oneLine, source)
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

	if _, found, err := readInstalledSkill(ctx, b, actor, name); err != nil {
		return "", err
	} else if found {
		ok, err := confirmAction(ctx, fmt.Sprintf("An installed skill named %q already exists. Overwrite it?", name), "Overwrite")
		if err != nil {
			return "", err
		}
		if !ok {
			return fmt.Sprintf("Skill %q not installed: kept the existing installed skill.", name), nil
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
	home := skillHomeDisplay(ctx, b, actor)
	// Pass content base64-encoded so the shell never interprets its bytes.
	payload := base64.StdEncoding.EncodeToString([]byte(marshalSkillFile(s)))
	args := []string{"/bin/sh", "-c", writeSkillScript, "skill-install", payload, name}
	if _, err := b.execResult(ctx, id, args, WorkspaceRoot, defaultExecTimeout); err != nil {
		return "", fmt.Errorf("write skill: %w", err)
	}
	return fmt.Sprintf("Installed skill %q to %s. Use Skill with action \"invoke\" to run it.", name, skillGuestPath(home, name)), nil
}

func skillRemove(ctx context.Context, b *AgentBinder, actor Actor, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	if _, found, err := readInstalledSkill(ctx, b, actor, name); err != nil {
		return "", err
	} else if !found {
		if _, builtin := builtinSkill(name); builtin {
			return "", fmt.Errorf("built-in skill %q cannot be removed; only an installed skill that shadows it can be deleted", name)
		}
		return "", fmt.Errorf("no skill %q is installed", name)
	}
	ok, err := confirmAction(ctx, fmt.Sprintf("Remove skill %q from the agent home?", name), "Remove")
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
	if _, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", removeSkillScript, "skill-rm", name}, WorkspaceRoot, defaultExecTimeout); err != nil {
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
