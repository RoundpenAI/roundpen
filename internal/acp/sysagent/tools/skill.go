package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Skill is a named, model-invocable prompt a user can run as a skill.
// It mirrors the Claude Code "Skill" slash-command concept: the model looks up
// a skill by name and follows the injected instructions.
type Skill struct {
	// Name is the identifier the model passes to the Skill tool.
	Name string
	// Description tells the model/catalog what the skill does.
	Description string
	// Prompt is the instruction text injected when the skill runs.
	Prompt string
	// Params marks that the skill accepts a free-form `args` value from the model.
	Params bool
}

// SkillCatalog is an in-memory registry of skills for the Skill tool.
type SkillCatalog struct {
	byName map[string]Skill
}

// NewSkillCatalog builds a catalog from skills.
func NewSkillCatalog(skills ...Skill) *SkillCatalog {
	c := &SkillCatalog{byName: make(map[string]Skill, len(skills))}
	for _, s := range skills {
		c.Add(s)
	}
	return c
}

// Add registers or replaces a skill.
func (c *SkillCatalog) Add(s Skill) {
	if c == nil {
		return
	}
	if c.byName == nil {
		c.byName = make(map[string]Skill)
	}
	c.byName[strings.TrimSpace(s.Name)] = s
}

// Get returns a skill by name.
func (c *SkillCatalog) Get(name string) (Skill, bool) {
	if c == nil || c.byName == nil {
		return Skill{}, false
	}
	s, ok := c.byName[strings.TrimSpace(name)]
	return s, ok
}

// List returns skills sorted by name.
func (c *SkillCatalog) List() []Skill {
	if c == nil || c.byName == nil {
		return nil
	}
	out := make([]Skill, 0, len(c.byName))
	for _, s := range c.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DefaultSkills returns the built-in skill set every System Agent gets.
func DefaultSkills() []Skill {
	return []Skill{
		{
			Name:        "commit",
			Description: "Review workspace changes and write a clear, conventional commit message.",
			Prompt: `Review the current git state in the Agent workspace:
1. Use Bash ` + "`git status --short`" + ` and ` + "`git diff`" + ` (staged + unstaged) to see what changed.
2. Summarize what the user is trying to accomplish from the conversation and the diff.
3. Create a concise commit message following the conventional commits style (feat/fix/refactor/docs/chore...).
4. Commit ONLY if the user explicitly asked to commit. Otherwise present the proposed message and ask whether to commit.`,
			Params: true,
		},
		{
			Name:        "review",
			Description: "Review the latest workspace changes for bugs, security, and clarity, then report findings.",
			Prompt: `Review the most recent changes in the Agent workspace:
1. Use Bash ` + "`git diff HEAD`" + ` and ` + "`git status --short`" + ` to scope the review; if there is no git history, list the workspace files with Glob and pick the relevant ones.
2. Read the changed files. Look for bugs, security issues, broken assumptions, missing error handling, and unclear code.
3. Report concise, prioritized findings with file:line references. Do not edit files during the review.`,
		},
		{
			Name:        "fix",
			Description: "Fix toolchain issues (build, lint, test failures) reported or found in the workspace.",
			Prompt: `Fix toolchain issues in the Agent workspace:
1. Reproduce the problem first: run the build, linter, or tests the user mentioned.
2. Read the errors and trace them to the responsible files before editing.
3. Apply minimal fixes that address root causes, then re-run to confirm.
4. If the issue is environmental rather than code (e.g. missing dependency), fix the environment with Bash and explain what you did.`,
			Params: true,
		},
		{
			Name:        "summarize",
			Description: "Summarize the current state of the workspace and the conversation.",
			Prompt: `Summarize the current workspace and conversation state:
1. Briefly walk the workspace layout (Glob top-level, read key files) to understand what the project is.
2. Combine what was already completed, what is in progress, and what remains.
3. Output a concise structured summary the user can resume from.`,
		},
	}
}

// RegisterSkill adds the Skill tool used to invoke named skills from the catalog.
// The tool result is a user-visible injection of the skill instructions so the
// model proceeds along the skill's workflow.
func RegisterSkill(r *Registry, catalog *SkillCatalog) {
	if r == nil || catalog == nil {
		return
	}
	r.Register(Tool{
		Name: "Skill",
		Description: "Invoke a named skill. Skills are curated workflows (reviewing, fixing, summarizing, committing) " +
			"with a fixed instruction set. When the task matches a skill, invoke it and follow the returned instructions.",
		Parameters: objectSchema(map[string]any{
			"skill": map[string]any{"type": "string", "description": "The skill name to invoke"},
			"args":  map[string]any{"type": "string", "description": "Optional free-form arguments for the skill"},
		}, "skill"),
		Call: func(_ context.Context, _ Actor, args json.RawMessage) (string, error) {
			var in struct {
				Skill string `json:"skill"`
				Args  string `json:"args"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			skill, ok := catalog.Get(in.Skill)
			if !ok {
				return "", fmt.Errorf("unknown skill %q; available skills: %s",
					strings.TrimSpace(in.Skill), strings.Join(skillNames(catalog), ", "))
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Skill %q\n\n%s", skill.Name, strings.TrimSpace(skill.Prompt))
			if skill.Params && strings.TrimSpace(in.Args) != "" {
				fmt.Fprintf(&b, "\n\nSkill arguments:\n%s", in.Args)
			}
			return b.String(), nil
		},
	})
}

func skillNames(c *SkillCatalog) []string {
	list := c.List()
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}
