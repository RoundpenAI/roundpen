package mcp

import (
	"strings"
	"testing"
)

func TestCatalogShape(t *testing.T) {
	list := Catalog()
	if len(list) != 14 {
		t.Fatalf("catalog size = %d, want 14", len(list))
	}
	ids := map[string]bool{}
	endpoints := map[string]bool{}
	groups := map[string]bool{}
	modes := map[AuthMode]int{}
	for _, s := range list {
		if s.ID == "" || s.Name == "" || s.Endpoint == "" || s.Description == "" || s.DocsURL == "" {
			t.Errorf("server %q has empty fields: %+v", s.ID, s)
		}
		if s.ID != strings.ToLower(s.ID) {
			t.Errorf("server id %q must be lowercase", s.ID)
		}
		if !strings.HasPrefix(s.Endpoint, "https://") {
			t.Errorf("server %q endpoint %q must be https", s.ID, s.Endpoint)
		}
		if ids[s.ID] {
			t.Errorf("duplicate server id %q", s.ID)
		}
		ids[s.ID] = true
		if endpoints[s.Endpoint] {
			t.Errorf("duplicate endpoint %q", s.Endpoint)
		}
		endpoints[s.Endpoint] = true
		groups[s.Group] = true
		switch s.Auth {
		case AuthOAuth, AuthAPIKey, AuthBoth:
			modes[s.Auth]++
		default:
			t.Errorf("server %q has unknown auth mode %q", s.ID, s.Auth)
		}
	}
	if len(groups) < 5 {
		t.Errorf("expected at least 5 groups, got %d", len(groups))
	}
	if modes[AuthOAuth] == 0 || modes[AuthAPIKey] == 0 || modes[AuthBoth] == 0 {
		t.Errorf("auth archetype coverage missing: %v", modes)
	}
}

func TestCatalogIsCopy(t *testing.T) {
	list := Catalog()
	list[0].Name = "mutated"
	if Catalog()[0].Name == "mutated" {
		t.Fatal("Catalog must return a copy")
	}
}

func TestLookup(t *testing.T) {
	if s, ok := Lookup("notion"); !ok || s.Name != "Notion" {
		t.Fatalf("Lookup(notion) = %+v, %v", s, ok)
	}
	if _, ok := Lookup(" Notion "); !ok {
		t.Fatal("Lookup should trim and lowercase")
	}
	if _, ok := Lookup("unknown"); ok {
		t.Fatal("Lookup(unknown) should fail")
	}
	if _, ok := Lookup(""); ok {
		t.Fatal("Lookup(empty) should fail")
	}
}
