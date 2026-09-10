package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestKernAPI_FilesCRUD(t *testing.T) {
	h := startKernHarness(t)

	resp := h.mustDo(t, http.MethodPost, "/v1/sandboxes", map[string]any{
		"templateID": "host",
		"timeout":    300,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: %s %s", resp.Status, b)
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	sid := created["sandboxID"].(string)

	req, err := http.NewRequest(http.MethodPost, h.URL+"/v1/sandboxes/"+sid+"/files?path=hello.txt", strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", h.APIKey)
	resp, err = h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("write: %s %s", resp.Status, b)
	}

	resp = h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+sid+"/files?path=.", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %s", resp.Status)
	}
	var listed struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range listed.Entries {
		if e["name"] == "hello.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing hello.txt in %#v", listed.Entries)
	}

	req, err = http.NewRequest(http.MethodGet, h.URL+"/v1/sandboxes/"+sid+"/files/content?path=hello.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", h.APIKey)
	resp, err = h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "hello world" {
		t.Fatalf("read: %s %q", resp.Status, body)
	}

	resp = h.mustDo(t, http.MethodDelete, "/v1/sandboxes/"+sid+"/files?path=hello.txt", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete: %s %s", resp.Status, b)
	}

	resp = h.mustDo(t, http.MethodDelete, "/v1/sandboxes/"+sid, nil)
	defer resp.Body.Close()
}
