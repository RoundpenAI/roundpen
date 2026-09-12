package hostsetup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestLLMReadyUnauthorized(t *testing.T) {
	mux := http.NewServeMux()
	(&Handler{Cfg: &config.Config{}}).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/setup/llm-ready", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", rr.Code)
	}
}

func TestCreatePlanEmptyActions(t *testing.T) {
	svc := &Service{
		Facts: func() HostFacts {
			return HostFacts{DockerReady: true, BinariesOK: true, BrowserImageOK: true}
		},
	}
	mux := http.NewServeMux()
	(&Handler{Svc: svc, Cfg: &config.Config{}}).Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/setup/plans", strings.NewReader(`{"preset":"code"}`))
	req = req.WithContext(auth.WithUser(req.Context(), &storage.User{Username: "alice"}))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	// Frontend SetupWorkstation calls plan.actions.every — null crashes to a white screen.
	if strings.Contains(body, `"actions":null`) {
		t.Fatalf("empty actions must serialize as [] not null: %s", body)
	}
	var rec PlanRecord
	if err := json.Unmarshal(rr.Body.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Actions == nil {
		t.Fatal("Actions must be non-nil empty slice")
	}
	if len(rec.Actions) != 0 {
		t.Fatalf("%#v", rec.Actions)
	}
}
