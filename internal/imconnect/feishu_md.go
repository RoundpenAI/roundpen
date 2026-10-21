package imconnect

import (
	"strings"
)

// flattenMarkdownTables rewrites Markdown pipe-tables into bullet lists.
// Feishu interactive cards render MD tables poorly (equal column widths,
// truncation, bad mobile layout); lists stay readable on Feishu / Weixin /
// Telegram alike.
func flattenMarkdownTables(s string) string {
	if s == "" || !strings.Contains(s, "|") {
		return s
	}
	lines := strings.Split(s, "\n")
	var out []string
	for i := 0; i < len(lines); {
		if isTableRow(lines[i]) {
			block, next := takeTableBlock(lines, i)
			if len(block) >= 2 {
				out = append(out, renderTableAsList(block)...)
				i = next
				continue
			}
		}
		out = append(out, lines[i])
		i++
	}
	return strings.Join(out, "\n")
}

func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 || t[0] != '|' || t[len(t)-1] != '|' {
		return false
	}
	return strings.Count(t, "|") >= 2
}

func isTableSep(line string) bool {
	t := strings.TrimSpace(line)
	if !isTableRow(t) {
		return false
	}
	cells := splitTableCells(t)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		c := strings.TrimSpace(cell)
		if c == "" || !strings.Contains(c, "-") {
			return false
		}
		for _, r := range c {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func splitTableCells(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	parts := strings.Split(t, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func takeTableBlock(lines []string, start int) (block []string, next int) {
	i := start
	for i < len(lines) && isTableRow(lines[i]) {
		block = append(block, lines[i])
		i++
	}
	return block, i
}

func renderTableAsList(block []string) []string {
	rows := make([][]string, 0, len(block))
	for _, line := range block {
		if isTableSep(line) {
			continue
		}
		rows = append(rows, splitTableCells(line))
	}
	if len(rows) == 0 {
		return nil
	}
	headers := rows[0]
	body := rows[1:]
	if len(body) == 0 {
		// Header-only: emit as a single bullet of column names.
		return []string{"- " + strings.Join(headers, " / ")}
	}
	out := make([]string, 0, len(body))
	for _, row := range body {
		parts := make([]string, 0, len(headers))
		for i, h := range headers {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			h = strings.TrimSpace(h)
			val = strings.TrimSpace(val)
			if h == "" {
				if val != "" {
					parts = append(parts, val)
				}
				continue
			}
			if val == "" {
				parts = append(parts, h)
				continue
			}
			parts = append(parts, h+": "+val)
		}
		if len(parts) == 0 {
			continue
		}
		out = append(out, "- "+strings.Join(parts, "；"))
	}
	return out
}
