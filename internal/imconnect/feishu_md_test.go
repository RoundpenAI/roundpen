package imconnect

import (
	"strings"
	"testing"
)

func TestFlattenMarkdownTables_Simple(t *testing.T) {
	in := "前置\n\n| 名 | 值 |\n|---|---|\n| a | 1 |\n| b | 2 |\n\n后置"
	got := flattenMarkdownTables(in)
	if strings.Contains(got, "|---") || strings.Contains(got, "| 名 |") {
		t.Fatalf("table markers remain: %q", got)
	}
	if !strings.Contains(got, "- 名: a；值: 1") {
		t.Fatalf("missing row list: %q", got)
	}
	if !strings.Contains(got, "- 名: b；值: 2") {
		t.Fatalf("missing second row: %q", got)
	}
	if !strings.Contains(got, "前置") || !strings.Contains(got, "后置") {
		t.Fatalf("surrounding text lost: %q", got)
	}
}

func TestFlattenMarkdownTables_LeavesNonTables(t *testing.T) {
	in := "use `|` in code and **bold**"
	if got := flattenMarkdownTables(in); got != in {
		t.Fatalf("got %q", got)
	}
}

func TestFlattenMarkdownTables_NoPipe(t *testing.T) {
	in := "plain text"
	if got := flattenMarkdownTables(in); got != in {
		t.Fatalf("got %q", got)
	}
}

func TestIsTableSep(t *testing.T) {
	if !isTableSep("|---|---|") {
		t.Fatal("expected sep")
	}
	if !isTableSep("| :--- | ---: |") {
		t.Fatal("expected aligned sep")
	}
	if isTableSep("| a | b |") {
		t.Fatal("data row is not sep")
	}
}
