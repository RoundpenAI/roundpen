package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxReadBytes = 100 << 10

// RegisterFiles adds Read, Write, and Edit tools for the Agent workspace.
func RegisterFiles(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name:        "Read",
		Description: "Read a text file from the Agent workspace (/workspace).",
		Parameters: objectSchema(map[string]any{
			"file_path": map[string]any{"type": "string", "description": "Absolute path under /workspace, or relative to /workspace"},
			"offset":    map[string]any{"type": "integer", "description": "1-based start line (optional)"},
			"limit":     map[string]any{"type": "integer", "description": "Max number of lines to return (optional)"},
		}, "file_path"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				FilePath string `json:"file_path"`
				Offset   int    `json:"offset"`
				Limit    int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.FilePath) == "" {
				return "", fmt.Errorf("file_path is required")
			}
			return binder.readFile(ctx, actor, in.FilePath, in.Offset, in.Limit)
		},
	})
	r.Register(Tool{
		Name:        "Write",
		Description: "Create or overwrite a text file in the Agent workspace (/workspace).",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"file_path": map[string]any{"type": "string", "description": "Absolute path under /workspace, or relative to /workspace"},
			"content":   map[string]any{"type": "string", "description": "Full file contents"},
		}, "file_path", "content"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				FilePath string `json:"file_path"`
				Content  string `json:"content"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.FilePath) == "" {
				return "", fmt.Errorf("file_path is required")
			}
			return binder.writeFile(ctx, actor, in.FilePath, in.Content)
		},
	})
	r.Register(Tool{
		Name:        "Edit",
		Description: "Make an exact string replacement in a file in the Agent workspace. old_string must match uniquely unless replace_all is true.",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"file_path":   map[string]any{"type": "string", "description": "Absolute path under /workspace, or relative to /workspace"},
			"old_string":  map[string]any{"type": "string", "description": "Exact text to find"},
			"new_string":  map[string]any{"type": "string", "description": "Replacement text"},
			"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence (default false)"},
		}, "file_path", "old_string", "new_string"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				FilePath   string `json:"file_path"`
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.FilePath) == "" {
				return "", fmt.Errorf("file_path is required")
			}
			return binder.editFile(ctx, actor, in.FilePath, in.OldString, in.NewString, in.ReplaceAll)
		},
	})
}

func (b *AgentBinder) readFile(ctx context.Context, actor Actor, filePath string, offset, limit int) (string, error) {
	rel, err := ResolveWorkspacePath(filePath)
	if err != nil {
		return "", err
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	if b.Files == nil {
		return "", fmt.Errorf("workspace files not configured")
	}
	rc, err := b.Files.ReadFile(ctx, id, rel)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, maxReadBytes+1))
	if err != nil {
		return "", err
	}
	truncated := false
	if len(raw) > maxReadBytes {
		raw = raw[:maxReadBytes]
		truncated = true
	}
	if len(raw) == 0 {
		return "File is empty.", nil
	}
	text := string(raw)
	lines := strings.Split(text, "\n")
	// Preserve trailing newline semantics: Split keeps a final empty element when text ends with \n.
	start := 0
	if offset > 0 {
		start = offset - 1
	}
	if start >= len(lines) {
		return fmt.Sprintf("Offset %d is past the end of the file (%d lines).", offset, len(lines)), nil
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	var bld strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&bld, "%6d|%s\n", i+1, lines[i])
	}
	out := strings.TrimSuffix(bld.String(), "\n")
	if truncated {
		out += "\n\n[File truncated at size limit; use offset/limit to read more.]"
	}
	return out, nil
}

func (b *AgentBinder) writeFile(ctx context.Context, actor Actor, filePath, content string) (string, error) {
	rel, err := ResolveWorkspacePath(filePath)
	if err != nil {
		return "", err
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	if b.Files == nil {
		return "", fmt.Errorf("workspace files not configured")
	}
	if err := b.Files.WriteFile(ctx, id, rel, strings.NewReader(content)); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), filePath), nil
}

func (b *AgentBinder) editFile(ctx context.Context, actor Actor, filePath, oldString, newString string, replaceAll bool) (string, error) {
	rel, err := ResolveWorkspacePath(filePath)
	if err != nil {
		return "", err
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	if b.Files == nil {
		return "", fmt.Errorf("workspace files not configured")
	}
	rc, err := b.Files.ReadFile(ctx, id, rel)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	raw, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return "", err
	}
	content := string(raw)
	n := strings.Count(content, oldString)
	if n == 0 {
		return "", fmt.Errorf("old_string not found in file")
	}
	if n > 1 && !replaceAll {
		return "", fmt.Errorf("old_string matched %d times; provide more context or set replace_all=true", n)
	}
	var next string
	if replaceAll {
		next = strings.ReplaceAll(content, oldString, newString)
	} else {
		next = strings.Replace(content, oldString, newString, 1)
	}
	if err := b.Files.WriteFile(ctx, id, rel, strings.NewReader(next)); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("Updated %s (%d replacement(s))", filePath, n), nil
}
