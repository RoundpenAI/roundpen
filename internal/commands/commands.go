// Package commands is the control-plane catalog of user slash commands.
//
// Two kinds exist: skill commands (the control plane expands a skill into
// instruction text and runs it as a user turn) and action commands (handled
// directly by the control plane, e.g. /clear, /help). The package is pure
// metadata plus display strings — no database or transport dependencies.
package commands

import (
	"fmt"
	"strings"
)

// Kind distinguishes how a command is executed.
type Kind string

const (
	KindSkill  Kind = "skill"
	KindAction Kind = "action"
)

// Sources reported in the catalog.
const (
	SourceBuiltin   = "builtin"
	SourceInstalled = "installed"
	SourceAction    = "action"
)

// Command is one catalog entry; the JSON shape is the /commands response item.
type Command struct {
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Args        bool   `json:"args"`
}

// Actions returns the built-in action commands.
func Actions() []Command {
	return []Command{
		{
			Name:        "clear",
			Kind:        KindAction,
			Source:      SourceAction,
			Description: "清空本会话上下文：聊天记录保留，模型从零开始",
		},
		{
			Name:        "help",
			Kind:        KindAction,
			Source:      SourceAction,
			Description: "显示可用命令",
		},
	}
}

// skillDisplayDescriptions holds Chinese display descriptions for the built-in
// skills. It is display-only: the English descriptions from DefaultSkills are
// model-visible (system prompt + Skill tool output) and must not change.
var skillDisplayDescriptions = map[string]string{
	"commit":    "审阅工作区改动并生成规范的提交信息",
	"review":    "审查最近改动，给出 bug / 安全 / 可读性结论",
	"fix":       "修复构建、lint、测试等工具链问题",
	"summarize": "总结工作区与当前会话的状态",
}

// SkillDisplayDescription returns the Chinese display description for a
// built-in skill, if one exists.
func SkillDisplayDescription(name string) (string, bool) {
	desc, ok := skillDisplayDescriptions[strings.TrimSpace(name)]
	return desc, ok
}

const argsHint = " [可带参数]"

// HelpText renders the /help output: action commands first, then skills.
func HelpText(cmds []Command) string {
	var actions, skills []Command
	for _, c := range cmds {
		if c.Kind == KindAction {
			actions = append(actions, c)
			continue
		}
		skills = append(skills, c)
	}

	var b strings.Builder
	b.WriteString("可用命令（在输入框打 / 唤起菜单）：")
	writeSection(&b, "动作命令", actions)
	if len(skills) > 0 {
		writeSection(&b, "技能命令", skills)
		b.WriteString("\n技能命令会把该技能的指令注入为你的一条消息；技能由模型用 Skill 工具安装与管理。")
	}
	return b.String()
}

func writeSection(b *strings.Builder, title string, cmds []Command) {
	if len(cmds) == 0 {
		return
	}
	width := 0
	for _, c := range cmds {
		if n := len(c.Name) + 1 + len(argsHint); n > width {
			width = n
		}
	}
	fmt.Fprintf(b, "\n\n%s", title)
	for _, c := range cmds {
		name := "/" + c.Name
		if c.Args {
			name += argsHint
		}
		fmt.Fprintf(b, "\n- %-*s  %s", width, name, c.Description)
	}
}
