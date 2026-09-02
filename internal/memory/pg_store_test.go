package memory_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/memory"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func testDB(t *testing.T) *storage.DB {
	t.Helper()
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type stubEmbedder struct {
	vec []float32
}

func (s stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = s.vec
	}
	return out, nil
}

func TestPgStoreShortAndLong(t *testing.T) {
	db := testDB(t)
	store := memory.NewPgStore(db)
	ctx := context.Background()

	sessionID := "sess-" + time.Now().Format("150405.000")
	agentID := "agent-" + time.Now().Format("150405.000")

	exp := time.Now().UTC().Add(time.Hour)
	short := memory.ShortEntry{
		ID: "short-1-" + sessionID, SessionID: sessionID,
		Payload: []byte(`{"note":"hello"}`), ExpiresAt: &exp, CreatedAt: time.Now().UTC(),
	}
	if err := store.PutShort(ctx, short); err != nil {
		t.Fatalf("PutShort: %v", err)
	}
	got, err := store.GetShort(ctx, short.ID)
	if err != nil {
		t.Fatalf("GetShort: %v", err)
	}
	if !jsonEqual(t, got.Payload, short.Payload) {
		t.Fatalf("GetShort payload: got %s want %s", got.Payload, short.Payload)
	}

	long := memory.LongEntry{
		ID: "long-1-" + agentID, AgentID: agentID, UserID: "alice",
		Kind: memory.LongFact, Content: "user prefers dark mode",
		Importance: 80, CreatedAt: time.Now().UTC(),
	}
	if err := store.PutLong(ctx, long, nil); err != nil {
		t.Fatalf("PutLong: %v", err)
	}
	gotL, err := store.GetLong(ctx, long.ID)
	if err != nil || gotL.UserID != "alice" {
		t.Fatalf("GetLong: %v %+v", err, gotL)
	}
	mems, err := store.ListLong(ctx, memory.LongFilter{AgentID: agentID})
	if err != nil || len(mems) < 1 {
		t.Fatalf("ListLong: %v len=%d", err, len(mems))
	}
	if err := store.DeleteLong(ctx, long.ID); err != nil {
		t.Fatal(err)
	}
	_ = store.DeleteShort(ctx, short.ID)
}

func TestServiceAddSearchWithEmbedder(t *testing.T) {
	db := testDB(t)
	store := memory.NewPgStore(db)
	ctx := context.Background()

	// 1024-dim stub vector
	vec := make([]float32, memory.EmbeddingDims)
	vec[0] = 1
	svc := &memory.Service{Store: store, Embed: stubEmbedder{vec: vec}}

	agentID := "agent-svc-" + time.Now().Format("150405.000")
	e, err := svc.Add(ctx, memory.AddInput{
		Text: "User lives in Austin", AgentID: agentID, UserID: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" {
		t.Fatal("empty id")
	}

	results, err := svc.Search(ctx, memory.SearchInput{
		Query:   "where does the user live?",
		Filters: memory.SearchFilter{AgentID: agentID, UserID: "alice"},
		TopK:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected search hits")
	}
	_ = store.DeleteLong(ctx, e.ID)
}

func TestContentFromMessagesViaAdd(t *testing.T) {
	db := testDB(t)
	store := memory.NewPgStore(db)
	svc := &memory.Service{Store: store}
	ctx := context.Background()
	agentID := "agent-msg-" + time.Now().Format("150405.000")

	e, err := svc.Add(ctx, memory.AddInput{
		AgentID: agentID,
		Messages: []memory.Message{
			{Role: "user", Content: "I moved to SF"},
			{Role: "assistant", Content: "Noted."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Content == "" {
		t.Fatal("empty content")
	}
	_ = store.DeleteLong(ctx, e.ID)
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	ab, _ := json.Marshal(va)
	bb, _ := json.Marshal(vb)
	return string(ab) == string(bb)
}
