package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/template"
)

func testTemplateService(t *testing.T) (*template.Service, func()) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ROUNDPEN_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set ROUNDPEN_TEST_DATABASE_URL (make dev configures roundpen_test)")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	if err := db.MigrateEmbedded(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("migrate: %v", err)
	}
	store := template.NewStore(db.SQL)
	svc := template.NewService(store, "host")
	if err := svc.Seed(ctx, "kern"); err != nil {
		_ = db.Close()
		t.Fatalf("seed: %v", err)
	}
	return svc, func() { _ = db.Close() }
}

func TestHandler_listTemplates(t *testing.T) {
	tplSvc, cleanup := testTemplateService(t)
	defer cleanup()

	mux := http.NewServeMux()
	(&Handler{Templates: tplSvc}).Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/templates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list []templateResp
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) < 5 {
		t.Fatalf("expected seeded templates, got %d", len(list))
	}
}

func TestHandler_createTemplateV3(t *testing.T) {
	tplSvc, cleanup := testTemplateService(t)
	defer cleanup()

	mux := http.NewServeMux()
	(&Handler{Templates: tplSvc}).Mount(mux)

	name := fmt.Sprintf("http-tpl-%s", uuid.NewString()[:8])
	body, _ := json.Marshal(map[string]any{
		"name":     name,
		"cpuCount": 2,
		"memoryMB": 2048,
	})
	req := httptest.NewRequest(http.MethodPost, "/v3/templates", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out createTemplateV3Resp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.TemplateID == "" || out.BuildID == "" {
		t.Fatalf("missing ids: %+v", out)
	}
}

func TestHandler_getTemplateBuildStatus_notFound(t *testing.T) {
	tplSvc, cleanup := testTemplateService(t)
	defer cleanup()

	mux := http.NewServeMux()
	(&Handler{Templates: tplSvc}).Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/templates/"+uuid.NewString()+"/builds/"+uuid.NewString()+"/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_startTemplateBuildV2_requiresBuilder(t *testing.T) {
	tplSvc, cleanup := testTemplateService(t)
	defer cleanup()

	ctx := context.Background()
	created, err := tplSvc.CreateTemplate(ctx, template.CreateTemplateRequest{
		Name: fmt.Sprintf("nobuild-http-%s", uuid.NewString()[:8]),
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	(&Handler{Templates: tplSvc}).Mount(mux)

	body, _ := json.Marshal(map[string]any{"fromImage": "alpine:3.20"})
	path := "/v2/templates/" + created.TemplateID + "/builds/" + created.BuildID
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_templateCRUD(t *testing.T) {
	tplSvc, cleanup := testTemplateService(t)
	defer cleanup()

	mux := http.NewServeMux()
	(&Handler{Templates: tplSvc}).Mount(mux)

	ctx := context.Background()
	created, err := tplSvc.CreateTemplate(ctx, template.CreateTemplateRequest{
		Name: fmt.Sprintf("crud-http-%s", uuid.NewString()[:8]),
		CPUCount: 1, MemoryMB: 512, Public: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/templates/"+created.TemplateID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}

	body, _ := json.Marshal(map[string]any{"description": "hello", "cpuCount": 2})
	req = httptest.NewRequest(http.MethodPatch, "/templates/"+created.TemplateID, bytes.NewReader(body))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/templates/"+created.TemplateID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_deleteBuiltinForbidden(t *testing.T) {
	tplSvc, cleanup := testTemplateService(t)
	defer cleanup()

	list, err := tplSvc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var hostID string
	for _, rec := range list {
		if rec.Name == "host" {
			hostID = rec.TemplateID
			break
		}
	}
	if hostID == "" {
		t.Fatal("host template missing")
	}

	mux := http.NewServeMux()
	(&Handler{Templates: tplSvc}).Mount(mux)

	req := httptest.NewRequest(http.MethodDelete, "/templates/"+hostID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
