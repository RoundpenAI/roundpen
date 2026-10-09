package webpush

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNotifyPrunesGoneEndpoint(t *testing.T) {
	db := pushTestDB(t)
	user := pushTestUser(t, db)
	store := &Store{DB: db.SQL}
	ctx := context.Background()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusGone)
	}))
	t.Cleanup(srv.Close)

	vapid, err := LoadOrGenerateVAPID(t.TempDir()+"/vapid.json", "", "", "mailto:test@localhost")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Upsert(ctx, user, UpsertInput{
		Endpoint: srv.URL + "/push",
		// Known-good URL-safe subscription keys from webpush-go tests.
		P256dh: "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk",
		Auth:   "zqbxT6JKstKSY9JKibZLSQ",
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		Store:  store,
		VAPID:  vapid,
		Client: srv.Client(),
	}
	svc.Notify(ctx, user, Payload{
		Type:  "permission",
		Title: "需要确认",
		Body:  "Bash",
		URL:   "/a/a1/s/s1",
	})

	if hits.Load() == 0 {
		t.Fatal("expected push HTTP request")
	}
	ok, err := store.HasAny(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("410 Gone subscription should have been deleted")
	}
}

func TestServiceHasSubscriptionRequiresConfigured(t *testing.T) {
	svc := &Service{}
	if svc.HasSubscription(context.Background(), "u") {
		t.Fatal("unconfigured service must report no subscription")
	}
	if (&Service{VAPID: &VAPID{PublicKey: "p", PrivateKey: "k"}}).Configured() != true {
		t.Fatal("expected configured")
	}
}
