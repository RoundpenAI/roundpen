// E2B protocol compatibility checks derived from E2B OpenAPI 0.1.0
// (github.com/e2b-dev/docs openapi-public.yml).
package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type compatVerdict string

const (
	verdictPass           compatVerdict = "pass"
	verdictPartial        compatVerdict = "partial"
	verdictFail           compatVerdict = "fail"
	verdictNotImplemented compatVerdict = "not_implemented"
	verdictSkip           compatVerdict = "skip"
)

type compatCase struct {
	ID          string
	Layer       string
	E2BRef      string
	Priority    string
	Implemented bool
	Run         func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string)
}

type compatCtx struct {
	sandboxID string
}

type compatResult struct {
	Case    compatCase
	Verdict compatVerdict
	Detail  string
}

func e2bCompatCases() []compatCase {
	return []compatCase{
		{
			ID: "platform.postSandboxes", Layer: "platform", E2BRef: "postSandboxes", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				resp := h.mustDo(t, http.MethodPost, "/sandboxes", map[string]any{
					"templateID": "host",
					"timeout":    300,
					"metadata":   map[string]string{"suite": "e2b-compat"},
					"envVars":    map[string]string{"FOO": "bar"},
				})
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusCreated {
					b, _ := io.ReadAll(resp.Body)
					return verdictFail, fmt.Sprintf("status=%s body=%s", resp.Status, b)
				}
				var sb map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
					return verdictFail, err.Error()
				}
				missing := missingJSONFields(sb, "sandboxID", "templateID", "clientID", "envdVersion", "startedAt", "endAt", "state")
				if len(missing) > 0 {
					return verdictPartial, "missing fields: " + strings.Join(missing, ", ")
				}
				id, _ := sb["sandboxID"].(string)
				if id == "" {
					return verdictFail, "empty sandboxID"
				}
				ctx.sandboxID = id
				return verdictPass, ""
			},
		},
		{
			ID: "platform.getSandbox", Layer: "platform", E2BRef: "getSandbox", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodGet, "/sandboxes/"+ctx.sandboxID, nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return verdictFail, resp.Status
				}
				var sb map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
					return verdictFail, err.Error()
				}
				missing := missingJSONFields(sb, "sandboxID", "templateID", "clientID", "envdVersion")
				for _, f := range []string{"startedAt", "endAt", "cpuCount", "memoryMB", "diskSizeMB"} {
					if _, ok := sb[f]; !ok {
						missing = append(missing, f)
					}
				}
				if len(missing) > 0 {
					return verdictPartial, "SandboxDetail gaps: " + strings.Join(missing, ", ")
				}
				return verdictPass, ""
			},
		},
		{
			ID: "platform.listSandboxes", Layer: "platform", E2BRef: "listSandboxes", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				resp := h.mustDo(t, http.MethodGet, "/sandboxes", nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return verdictFail, resp.Status
				}
				var list []map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
					return verdictFail, err.Error()
				}
				if ctx.sandboxID != "" && !sandboxInList(list, ctx.sandboxID) {
					return verdictFail, "created sandbox not listed"
				}
				if len(list) > 0 {
					var gaps []string
					for _, f := range []string{"startedAt", "endAt", "cpuCount", "memoryMB", "diskSizeMB", "state"} {
						if _, ok := list[0][f]; !ok {
							gaps = append(gaps, f)
						}
					}
					if len(gaps) > 0 {
						return verdictPartial, "ListedSandbox gaps: " + strings.Join(gaps, ", ")
					}
				}
				return verdictPass, ""
			},
		},
		{
			ID: "platform.getHealth", Layer: "platform", E2BRef: "getHealth", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				resp := h.mustDo(t, http.MethodGet, "/health", nil)
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusNoContent {
					return verdictPass, ""
				}
				if resp.StatusCode == http.StatusOK {
					return verdictPartial, "status 200 text (E2B envd expects 204)"
				}
				return verdictFail, resp.Status
			},
		},
		{
			ID: "envd.processStart", Layer: "envd", E2BRef: "process.Process.Start", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodPost, "/v1/sandboxes/"+ctx.sandboxID+"/exec", map[string]any{
					"command": []string{"/bin/sh", "-c", "echo e2b-compat"},
					"timeout": 30,
				})
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					b, _ := io.ReadAll(resp.Body)
					return verdictFail, fmt.Sprintf("%s %s", resp.Status, b)
				}
				return verdictPartial, "REST POST /v1/sandboxes/{id}/exec (not ConnectRPC /process.Process/Start)"
			},
		},
		{
			ID: "envd.filesystemListDir", Layer: "envd", E2BRef: "filesystem.Filesystem.ListDir", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+ctx.sandboxID+"/files?path=.", nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return verdictFail, resp.Status
				}
				return verdictPartial, "REST GET /v1/sandboxes/{id}/files (not ConnectRPC ListDir)"
			},
		},
		{
			ID: "platform.postSandboxConnect", Layer: "platform", E2BRef: "postSandboxConnect", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodPost, "/sandboxes/"+ctx.sandboxID+"/connect", map[string]any{})
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
					b, _ := io.ReadAll(resp.Body)
					return verdictFail, fmt.Sprintf("%s %s", resp.Status, b)
				}
				return verdictPass, ""
			},
		},
		{
			ID: "platform.postSandboxPause", Layer: "platform", E2BRef: "postSandboxPause", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				return probeNotImplemented(t, h, http.MethodPost, "/sandboxes/"+ctx.sandboxID+"/pause", nil)
			},
		},
		{
			ID: "platform.getSandboxMetrics", Layer: "platform", E2BRef: "getSandboxMetrics", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				return probeNotImplemented(t, h, http.MethodGet, "/sandboxes/"+ctx.sandboxID+"/metrics")
			},
		},
		{
			ID: "platform.putSandboxNetwork", Layer: "platform", E2BRef: "putSandboxNetwork", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				return probeNotImplemented(t, h, http.MethodPut, "/sandboxes/"+ctx.sandboxID+"/network", map[string]any{})
			},
		},
		{
			ID: "platform.listSandboxesV2", Layer: "platform", E2BRef: "listSandboxesV2", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				return probeNotImplemented(t, h, http.MethodGet, "/v2/sandboxes")
			},
		},
		{
			ID: "platform.listSandboxesMetrics", Layer: "platform", E2BRef: "listSandboxesMetrics", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				return probeNotImplemented(t, h, http.MethodGet, "/sandboxes/metrics")
			},
		},
		{
			ID: "platform.getSandboxLogs", Layer: "platform", E2BRef: "getSandboxLogs", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				return probeNotImplemented(t, h, http.MethodGet, "/sandboxes/"+ctx.sandboxID+"/logs")
			},
		},
		{
			ID: "platform.postSandboxRefreshes", Layer: "platform", E2BRef: "postSandboxRefreshes", Priority: "extended",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodPost, "/sandboxes/"+ctx.sandboxID+"/refreshes", nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					b, _ := io.ReadAll(resp.Body)
					return verdictFail, fmt.Sprintf("%s %s", resp.Status, b)
				}
				return verdictPass, ""
			},
		},
		{
			ID: "platform.postSandboxSnapshots", Layer: "platform", E2BRef: "postSandboxSnapshots", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				return probeNotImplemented(t, h, http.MethodPost, "/sandboxes/"+ctx.sandboxID+"/snapshots", nil)
			},
		},
		{
			ID: "platform.listTemplates", Layer: "platform", E2BRef: "listTemplates", Priority: "extended",
			Implemented: true,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				resp := h.mustDo(t, http.MethodGet, "/templates", nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return verdictFail, resp.Status
				}
				var list []map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
					return verdictFail, err.Error()
				}
				if len(list) == 0 {
					return verdictFail, "empty template list"
				}
				missing := missingJSONFields(list[0], "templateID", "buildID", "cpuCount", "memoryMB", "diskSizeMB", "buildStatus", "envdVersion")
				if len(missing) > 0 {
					return verdictPartial, "Template gaps: " + strings.Join(missing, ", ")
				}
				return verdictPass, ""
			},
		},
		{
			ID: "envd.processList", Layer: "envd", E2BRef: "process.Process.List", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				return probeNotImplemented(t, h, http.MethodPost, "/process.Process/List", map[string]any{})
			},
		},
		{
			ID: "envd.filesystemMakeDir", Layer: "envd", E2BRef: "filesystem.Filesystem.MakeDir", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				return probeNotImplemented(t, h, http.MethodPost, "/filesystem.Filesystem/MakeDir", map[string]any{})
			},
		},
		{
			ID: "platform.postSandboxTimeout", Layer: "platform", E2BRef: "postSandboxTimeout", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodPost, "/sandboxes/"+ctx.sandboxID+"/timeout", map[string]any{"timeout": 600})
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					b, _ := io.ReadAll(resp.Body)
					return verdictFail, fmt.Sprintf("%s %s", resp.Status, b)
				}
				return verdictPass, ""
			},
		},
		{
			ID: "roundpen.resolveSandbox", Layer: "roundpen-ext", E2BRef: "—", Priority: "extended",
			Implemented: true,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				resp := h.mustDo(t, http.MethodPost, "/sandboxes", map[string]any{
					"templateID": "host", "timeout": 120, "name": "compat-resolve", "category": "E2B",
				})
				defer resp.Body.Close()
				var sb map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&sb)
				id, _ := sb["sandboxID"].(string)
				resp = h.mustDo(t, http.MethodGet, "/sandboxes/resolve?name=compat-resolve", nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return verdictFail, resp.Status
				}
				_ = h.mustDo(t, http.MethodDelete, "/sandboxes/"+id, nil).Body.Close()
				return verdictPass, "Roundpen extension"
			},
		},
		{
			ID: "roundpen.patchSandbox", Layer: "roundpen-ext", E2BRef: "—", Priority: "extended",
			Implemented: true,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				resp := h.mustDo(t, http.MethodPost, "/sandboxes", map[string]any{"templateID": "host", "timeout": 120})
				defer resp.Body.Close()
				var sb map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&sb)
				id, _ := sb["sandboxID"].(string)
				resp = h.mustDo(t, http.MethodPatch, "/sandboxes/"+id, map[string]any{"name": "patched"})
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					b, _ := io.ReadAll(resp.Body)
					return verdictFail, fmt.Sprintf("%s %s", resp.Status, b)
				}
				_ = h.mustDo(t, http.MethodDelete, "/sandboxes/"+id, nil).Body.Close()
				return verdictPass, "Roundpen extension"
			},
		},
		{
			ID: "envd.filesystemStat", Layer: "envd", E2BRef: "filesystem.Filesystem.Stat", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodGet, "/v1/sandboxes/"+ctx.sandboxID+"/files/stat?path=.", nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return verdictFail, resp.Status
				}
				return verdictPartial, "REST GET /v1/sandboxes/{id}/files/stat (not ConnectRPC Stat)"
			},
		},
		{
			ID: "envd.getEnvVars", Layer: "envd", E2BRef: "getEnvVars", Priority: "extended",
			Implemented: false,
			Run: func(t *testing.T, h *harness, _ *compatCtx) (compatVerdict, string) {
				return probeNotImplemented(t, h, http.MethodGet, "/envs")
			},
		},
		{
			ID: "platform.deleteSandbox", Layer: "platform", E2BRef: "deleteSandbox", Priority: "core",
			Implemented: true,
			Run: func(t *testing.T, h *harness, ctx *compatCtx) (compatVerdict, string) {
				if ctx.sandboxID == "" {
					return verdictSkip, "no sandbox"
				}
				resp := h.mustDo(t, http.MethodDelete, "/sandboxes/"+ctx.sandboxID, nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					b, _ := io.ReadAll(resp.Body)
					return verdictFail, fmt.Sprintf("%s %s", resp.Status, b)
				}
				resp = h.mustDo(t, http.MethodGet, "/sandboxes/"+ctx.sandboxID, nil)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNotFound {
					return verdictPartial, "delete ok but get returns " + resp.Status
				}
				ctx.sandboxID = ""
				return verdictPass, ""
			},
		},
	}
}

func probeNotImplemented(t *testing.T, h *harness, method, path string, body ...any) (compatVerdict, string) {
	var b any
	if len(body) > 0 {
		b = body[0]
	}
	resp := h.mustDo(t, method, path, b)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return verdictNotImplemented, resp.Status
	}
	return verdictPartial, "unexpected " + resp.Status
}

func missingJSONFields(obj map[string]any, fields ...string) []string {
	var missing []string
	for _, f := range fields {
		if v, ok := obj[f]; !ok || v == nil || v == "" {
			missing = append(missing, f)
		}
	}
	return missing
}

func sandboxInList(list []map[string]any, id string) bool {
	for _, sb := range list {
		if sb["sandboxID"] == id {
			return true
		}
	}
	return false
}

func scoreResults(results []compatResult) (implementedScore, fullScore float64, summary string) {
	var implTotal, implPoints, fullTotal, fullPoints float64
	var pass, partial, fail, notImpl, skip int
	for _, r := range results {
		weight := 1.0
		if r.Case.Priority == "extended" {
			weight = 0.5
		}
		fullTotal += weight
		switch r.Verdict {
		case verdictPass:
			fullPoints += weight
			pass++
		case verdictPartial:
			fullPoints += weight * 0.5
			partial++
		case verdictFail:
			fail++
		case verdictNotImplemented:
			notImpl++
		case verdictSkip:
			skip++
		}
		if r.Case.Implemented {
			implTotal += weight
			switch r.Verdict {
			case verdictPass:
				implPoints += weight
			case verdictPartial:
				implPoints += weight * 0.5
			case verdictSkip:
				implTotal -= weight
			}
		}
	}
	if implTotal > 0 {
		implementedScore = implPoints / implTotal * 100
	}
	if fullTotal > 0 {
		fullScore = fullPoints / fullTotal * 100
	}
	summary = fmt.Sprintf("pass=%d partial=%d fail=%d not_implemented=%d skip=%d", pass, partial, fail, notImpl, skip)
	return implementedScore, fullScore, summary
}

func scoreByLayer(results []compatResult) map[string]float64 {
	type acc struct{ total, points float64 }
	m := map[string]*acc{}
	for _, r := range results {
		if r.Verdict == verdictSkip {
			continue
		}
		a := m[r.Case.Layer]
		if a == nil {
			a = &acc{}
			m[r.Case.Layer] = a
		}
		w := 1.0
		if r.Case.Priority == "extended" {
			w = 0.5
		}
		a.total += w
		switch r.Verdict {
		case verdictPass:
			a.points += w
		case verdictPartial:
			a.points += w * 0.5
		}
	}
	out := map[string]float64{}
	for layer, a := range m {
		if a.total > 0 {
			out[layer] = a.points / a.total * 100
		}
	}
	return out
}
