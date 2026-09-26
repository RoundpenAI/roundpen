package oauth

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// fakeStore is an in-memory store mirroring the constraints of the PG one.
type fakeStore struct {
	providers  map[string]Provider
	identities []Identity
	states     map[string]State
}

func newFakeStore(providers ...Provider) *fakeStore {
	f := &fakeStore{providers: map[string]Provider{}, states: map[string]State{}}
	for _, p := range providers {
		f.providers[p.ID] = p
	}
	return f
}

func (f *fakeStore) ListProviders(_ context.Context, enabledOnly bool) ([]Provider, error) {
	var out []Provider
	for _, p := range f.providers {
		if enabledOnly && !p.Enabled {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeStore) GetProvider(_ context.Context, id string) (*Provider, error) {
	p, ok := f.providers[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &p, nil
}

func (f *fakeStore) SaveProvider(_ context.Context, p Provider) (*Provider, error) {
	f.providers[p.ID] = p
	return &p, nil
}

func (f *fakeStore) DeleteProvider(_ context.Context, id string) error {
	if _, ok := f.providers[id]; !ok {
		return storage.ErrNotFound
	}
	delete(f.providers, id)
	return nil
}

// enrich mirrors the JOIN the PG store does: provider fields ride along.
func (f *fakeStore) enrich(id *Identity) {
	if p, ok := f.providers[id.ProviderID]; ok {
		id.ProviderKind, id.ProviderHost, id.ProviderLabel = p.Kind, p.Host, p.Label
	}
}

func (f *fakeStore) ListIdentities(_ context.Context, userID string) ([]Identity, error) {
	var out []Identity
	for _, id := range f.identities {
		if id.UserID == userID {
			f.enrich(&id)
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeStore) GetIdentityBySubject(_ context.Context, providerID, subject string) (*Identity, error) {
	for _, id := range f.identities {
		if id.ProviderID == providerID && id.Subject == subject {
			cp := id
			f.enrich(&cp)
			return &cp, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (f *fakeStore) DeleteIdentityForProvider(_ context.Context, userID, providerID string) error {
	for i := range f.identities {
		if f.identities[i].UserID == userID && f.identities[i].ProviderID == providerID {
			f.identities = append(f.identities[:i], f.identities[i+1:]...)
			return nil
		}
	}
	return storage.ErrNotFound
}

func (f *fakeStore) UpsertIdentity(_ context.Context, in IdentityUpsert) (*Identity, error) {
	for i := range f.identities {
		id := &f.identities[i]
		if id.ProviderID != in.ProviderID || id.Subject != in.Subject {
			continue
		}
		if id.UserID != in.UserID {
			return nil, ErrLinkedElsewhere
		}
		id.Login, id.Name, id.Email = in.Login, in.Name, in.Email
		id.AccessToken = in.AccessToken
		if in.RefreshToken != "" {
			id.RefreshToken = in.RefreshToken
		}
		id.TokenExpiresAt = in.ExpiresAt
		id.Scopes = in.Scopes
		cp := *id
		return &cp, nil
	}
	for _, id := range f.identities {
		if id.UserID == in.UserID && id.ProviderID == in.ProviderID {
			return nil, ErrAlreadyLinked
		}
	}
	row := Identity{
		ID: "ident-" + in.Subject, UserID: in.UserID, ProviderID: in.ProviderID,
		Subject: in.Subject, Login: in.Login, Name: in.Name, Email: in.Email,
		AccessToken: in.AccessToken, RefreshToken: in.RefreshToken,
		TokenExpiresAt: in.ExpiresAt, Scopes: in.Scopes,
	}
	f.identities = append(f.identities, row)
	return &row, nil
}

func (f *fakeStore) SaveIdentityTokens(_ context.Context, id, access, refresh string, expires *time.Time) error {
	for i := range f.identities {
		if f.identities[i].ID == id {
			f.identities[i].AccessToken = access
			if refresh != "" {
				f.identities[i].RefreshToken = refresh
			}
			f.identities[i].TokenExpiresAt = expires
			return nil
		}
	}
	return storage.ErrNotFound
}

func (f *fakeStore) DeleteIdentity(_ context.Context, userID, id string) error {
	for i := range f.identities {
		if f.identities[i].ID == id && f.identities[i].UserID == userID {
			f.identities = append(f.identities[:i], f.identities[i+1:]...)
			return nil
		}
	}
	return storage.ErrNotFound
}

func (f *fakeStore) ListExpiringIdentities(_ context.Context, before time.Time) ([]Identity, error) {
	var out []Identity
	for _, id := range f.identities {
		if id.TokenExpiresAt != nil && id.TokenExpiresAt.Before(before) && id.RefreshToken != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateState(_ context.Context, st State) error {
	f.states[st.State] = st
	return nil
}

func (f *fakeStore) TakeState(_ context.Context, state string) (*State, error) {
	st, ok := f.states[state]
	if !ok || !st.ExpiresAt.After(time.Now()) {
		return nil, storage.ErrNotFound
	}
	delete(f.states, state)
	return &st, nil
}

// flowFixture wires a service against a fake remote and the memory user store.
type flowFixture struct {
	svc   *Service
	store *fakeStore
	users *storage.MemoryUserStore
	prov  Provider
	web   *httptest.Server
	allow bool
}

func newFlowFixture(t *testing.T, remote *fakeRemote) *flowFixture {
	t.Helper()
	srv := remote.start()
	t.Cleanup(srv.Close)

	base := strings.TrimPrefix(srv.URL, "http://")
	p, err := Normalize(Provider{Kind: KindGitea, Scheme: "http", Host: base, ClientID: "cid", ClientSecret: "sec", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	f := &flowFixture{
		store: newFakeStore(p),
		users: storage.NewMemoryUserStore(),
		prov:  p,
		web:   srv,
		allow: true,
	}
	f.svc = &Service{
		Store:             f.store,
		Users:             f.users,
		AllowRegistration: func() bool { return f.allow },
	}
	return f
}

func (f *flowFixture) addUser(t *testing.T, u storage.User) {
	t.Helper()
	if u.APIKey == "" {
		u.APIKey = "rp-" + u.Username
	}
	if u.Role == "" {
		u.Role = storage.RoleUser
	}
	if err := f.users.Upsert(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}

func (f *flowFixture) start(t *testing.T, in StartInput) string {
	t.Helper()
	if in.ProviderID == "" {
		in.ProviderID = f.prov.ID
	}
	if in.ConsoleScheme == "" {
		in.ConsoleScheme = "https"
		in.ConsoleHost = "console.test"
	}
	raw, err := f.svc.AuthorizeURL(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func stateFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

func callback(t *testing.T, f *flowFixture, state, sessionUser string) (*CallbackResult, error) {
	t.Helper()
	return f.svc.HandleCallback(context.Background(), CallbackInput{
		ProviderID:  f.prov.ID,
		Code:        "the-code",
		State:       state,
		SessionUser: sessionUser,
	})
}

func TestAuthorizeURLStoresStateWithVerifierAndRedirect(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t, tokenOK: func(map[string]string) (int, string) {
		return 200, `{"access_token":"at"}`
	}})
	raw := f.start(t, StartInput{RedirectTo: "/settings/accounts"})
	state := stateFromURL(t, raw)
	if state == "" {
		t.Fatal("authorize url has no state")
	}
	st, err := f.svc.Store.TakeState(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if st.Verifier == "" {
		t.Error("state should carry the PKCE verifier")
	}
	if want := f.prov.CallbackURL("https", "console.test"); st.RedirectURI != want {
		t.Errorf("redirect uri = %q, want %q", st.RedirectURI, want)
	}
	if st.RedirectTo != "/settings/accounts" {
		t.Errorf("redirect to = %q", st.RedirectTo)
	}
}

func TestCallbackSignsInExistingIdentity(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":42,"login":"octo","full_name":"Octo","email":"octo@example.test"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})
	f.addUser(t, storage.User{Username: "alice", Email: "alice@example.test"})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "42", Login: "octo",
	}); err != nil {
		t.Fatal(err)
	}
	state := stateFromURL(t, f.start(t, StartInput{}))

	res, err := callback(t, f, state, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Username != "alice" {
		t.Errorf("user = %s", res.User.Username)
	}
	if res.Linked {
		t.Error("a login should not be reported as a link")
	}
	got, err := f.store.GetIdentityBySubject(context.Background(), f.prov.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken == "" {
		t.Errorf("identity token not stored: %+v", got)
	}
}

func TestCallbackLinksByVerifiedEmail(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":7,"login":"octo","email":"public@example.test"}`,
		emails: `[{"email":"alice@example.test","primary":true,"verified":true}]`,
	})
	f.addUser(t, storage.User{Username: "alice", Email: "Alice@Example.test"})

	res, err := callback(t, f, stateFromURL(t, f.start(t, StartInput{})), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Username != "alice" {
		t.Errorf("user = %s, want alice (verified email match)", res.User.Username)
	}
}

func TestCallbackIgnoresUnverifiedEmailForLinking(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:            t,
		user:         `{"id":7,"login":"octo","email":"alice@example.test"}`,
		emailsStatus: 403,
	})
	f.addUser(t, storage.User{Username: "alice", Email: "alice@example.test"})

	res, err := callback(t, f, stateFromURL(t, f.start(t, StartInput{})), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Username == "alice" {
		t.Error("the profile email is not verified and must not claim an existing account")
	}
	if res.User.Username != "octo" {
		t.Errorf("user = %s, want a new account named octo", res.User.Username)
	}
}

func TestCallbackCreatesUserWhenRegistrationAllowed(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":9,"login":"Octo Cat!","full_name":"Octo Cat","email":"octo@example.test"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})

	res, err := callback(t, f, stateFromURL(t, f.start(t, StartInput{})), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Username != "Octo_Cat" {
		t.Errorf("username = %q, want the sanitized remote login", res.User.Username)
	}
	if res.User.AuthProvider != KindGitea {
		t.Errorf("auth provider = %q", res.User.AuthProvider)
	}
	if res.User.Email != "octo@example.test" {
		t.Errorf("email = %q", res.User.Email)
	}
	if res.User.PasswordHash != "" {
		t.Error("an oauth account must not get a password")
	}
	if !strings.HasPrefix(res.User.APIKey, "rp-") {
		t.Errorf("api key = %q", res.User.APIKey)
	}
}

func TestCallbackRefusesWhenRegistrationDisabled(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":9,"login":"octo"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})
	f.allow = false

	_, err := callback(t, f, stateFromURL(t, f.start(t, StartInput{})), "")
	if !errors.Is(err, ErrRegistrationDisabled) {
		t.Fatalf("err = %v, want ErrRegistrationDisabled", err)
	}
	if _, gerr := f.users.GetByUsername(context.Background(), "octo"); !errors.Is(gerr, storage.ErrNotFound) {
		t.Error("no account should have been created")
	}
}

func TestCallbackLinkFlow(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":5,"login":"octo"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})
	f.addUser(t, storage.User{Username: "alice", Email: "alice@example.test"})

	// The flow was started in alice's session.
	state := stateFromURL(t, f.start(t, StartInput{LinkUser: "alice", RedirectTo: "/settings/accounts"}))

	if _, err := callback(t, f, state, "bob"); !errors.Is(err, ErrStateUserMismatch) {
		t.Fatalf("err = %v, want ErrStateUserMismatch", err)
	}

	state = stateFromURL(t, f.start(t, StartInput{LinkUser: "alice", RedirectTo: "/settings/accounts"}))
	res, err := callback(t, f, state, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked || res.User.Username != "alice" {
		t.Errorf("result = %+v", res)
	}
	if res.RedirectTo != "/settings/accounts" {
		t.Errorf("redirect = %q", res.RedirectTo)
	}
}

func TestCallbackLinkFlowReplacesAccountOnSameProvider(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":5,"login":"octo"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})
	f.addUser(t, storage.User{Username: "alice"})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1", Login: "other",
		AccessToken: "old-token",
	}); err != nil {
		t.Fatal(err)
	}
	state := stateFromURL(t, f.start(t, StartInput{LinkUser: "alice"}))

	res, err := callback(t, f, state, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked || res.User.Username != "alice" {
		t.Errorf("result = %+v", res)
	}
	idents, err := f.store.ListIdentities(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(idents) != 1 || idents[0].Subject != "5" {
		t.Fatalf("identities = %+v, want exactly the new account", idents)
	}
	if _, err := f.store.GetIdentityBySubject(context.Background(), f.prov.ID, "1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("replaced account still bound: err = %v", err)
	}
}

func TestCallbackLinkFlowRefreshesExistingAccount(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":5,"login":"octo"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})
	f.addUser(t, storage.User{Username: "alice"})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "5", Login: "octo",
		AccessToken: "stale-token",
	}); err != nil {
		t.Fatal(err)
	}
	state := stateFromURL(t, f.start(t, StartInput{LinkUser: "alice"}))

	res, err := callback(t, f, state, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked || res.User.Username != "alice" {
		t.Errorf("result = %+v", res)
	}
	idents, err := f.store.ListIdentities(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(idents) != 1 || idents[0].AccessToken == "stale-token" {
		t.Fatalf("identities = %+v, want the same account with fresh tokens", idents)
	}
}

func TestCallbackLinkFlowRejectsSomeoneElsesAccount(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":5,"login":"octo"}`,
		emails: `[{"email":"octo@example.test","primary":true,"verified":true}]`,
	})
	f.addUser(t, storage.User{Username: "alice"})
	f.addUser(t, storage.User{Username: "bob"})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "5", Login: "octo",
	}); err != nil {
		t.Fatal(err)
	}
	state := stateFromURL(t, f.start(t, StartInput{LinkUser: "bob"}))

	// A link flow must not hand bob alice's session, nor steal alice's binding.
	if _, err := callback(t, f, state, "bob"); !errors.Is(err, ErrLinkedElsewhere) {
		t.Fatalf("err = %v, want ErrLinkedElsewhere", err)
	}
	got, err := f.store.GetIdentityBySubject(context.Background(), f.prov.ID, "5")
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "alice" {
		t.Errorf("identity moved to %q", got.UserID)
	}
}

func TestCallbackRejectsUnknownStateAndReplay(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t:      t,
		user:   `{"id":1,"login":"octo"}`,
		emails: `[]`,
	})
	if _, err := callback(t, f, "not-a-state", ""); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("err = %v, want ErrStateInvalid", err)
	}

	state := stateFromURL(t, f.start(t, StartInput{}))
	if _, err := callback(t, f, state, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := callback(t, f, state, ""); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("replayed state err = %v, want ErrStateInvalid", err)
	}
}

func TestCallbackRejectsDisabledProvider(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t, user: `{"id":1,"login":"octo"}`})
	f.addUser(t, storage.User{Username: "alice"})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1",
	}); err != nil {
		t.Fatal(err)
	}
	state := stateFromURL(t, f.start(t, StartInput{}))

	disabled := f.prov
	disabled.Enabled = false
	f.store.providers[f.prov.ID] = disabled

	if _, err := callback(t, f, state, ""); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err = %v, want ErrProviderUnavailable", err)
	}
}

func TestCredsForUserRefreshesExpiringToken(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t: t,
		tokenOK: func(form map[string]string) (int, string) {
			if form["grant_type"] != "refresh_token" {
				t.Errorf("grant_type = %q", form["grant_type"])
			}
			return 200, `{"access_token":"fresh-token","refresh_token":"rt-2","expires_in":3600}`
		},
	})
	f.addUser(t, storage.User{Username: "alice"})
	past := time.Now().Add(-time.Minute)
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1", Login: "octo",
		AccessToken: "stale-token", RefreshToken: "rt-1", ExpiresAt: &past,
	}); err != nil {
		t.Fatal(err)
	}

	creds, err := f.svc.CredsForUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 {
		t.Fatalf("creds = %+v", creds)
	}
	if creds[0].Token != "fresh-token" {
		t.Errorf("token = %q, want the refreshed one", creds[0].Token)
	}
	if creds[0].Host != f.prov.Host || creds[0].Provider != KindGitea {
		t.Errorf("cred = %+v", creds[0])
	}
	got, err := f.store.GetIdentityBySubject(context.Background(), f.prov.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "fresh-token" || got.RefreshToken != "rt-2" {
		t.Errorf("identity tokens not persisted: %+v", got)
	}
}

func TestCredsForUserKeepsTokenWhenRefreshFails(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t: t,
		tokenOK: func(map[string]string) (int, string) {
			return 400, `{"error":"invalid_grant"}`
		},
	})
	f.addUser(t, storage.User{Username: "alice"})
	past := time.Now().Add(-time.Minute)
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1",
		AccessToken: "stale-token", RefreshToken: "rt-1", ExpiresAt: &past,
	}); err != nil {
		t.Fatal(err)
	}

	creds, err := f.svc.CredsForUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Token != "stale-token" {
		t.Errorf("creds = %+v, want the stale token kept", creds)
	}
}

func TestCredsForUserSkipsDisabledProvider(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t})
	f.addUser(t, storage.User{Username: "alice"})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1", AccessToken: "at",
	}); err != nil {
		t.Fatal(err)
	}
	disabled := f.prov
	disabled.Enabled = false
	f.store.providers[f.prov.ID] = disabled

	creds, err := f.svc.CredsForUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 0 {
		t.Fatalf("creds = %+v, want none for a disabled provider", creds)
	}
}

func TestRefreshExpiringReportsAffectedUsers(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{
		t: t,
		tokenOK: func(map[string]string) (int, string) {
			return 200, `{"access_token":"fresh","expires_in":3600}`
		},
	})
	f.addUser(t, storage.User{Username: "alice"})
	f.addUser(t, storage.User{Username: "bob"})
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)
	for _, user := range []string{"alice", "bob"} {
		exp := &past
		if user == "bob" {
			exp = &future
		}
		if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
			UserID: user, ProviderID: f.prov.ID, Subject: user,
			AccessToken: "stale", RefreshToken: "rt", ExpiresAt: exp,
		}); err != nil {
			t.Fatal(err)
		}
	}

	users, err := f.svc.RefreshExpiring(context.Background(), time.Now().Add(refreshWindow))
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0] != "alice" {
		t.Errorf("users = %v, want only alice", users)
	}
}

func TestCallbackReportsIdentityChange(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t, user: `{"id":9,"login":"octo"}`})
	f.addUser(t, storage.User{Username: "alice"})
	var changed []string
	f.svc.OnIdentityChange = func(_ context.Context, userID string) { changed = append(changed, userID) }

	state := stateFromURL(t, f.start(t, StartInput{LinkUser: "alice"}))
	res, err := callback(t, f, state, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Linked {
		t.Error("link flow should report Linked")
	}
	if len(changed) != 1 || changed[0] != "alice" {
		t.Errorf("changed = %v, want [alice] after the link stored the token", changed)
	}
}

func TestUnlinkReportsIdentityChange(t *testing.T) {
	f := newFlowFixture(t, &fakeRemote{t: t})
	f.addUser(t, storage.User{Username: "alice"})
	if _, err := f.store.UpsertIdentity(context.Background(), IdentityUpsert{
		UserID: "alice", ProviderID: f.prov.ID, Subject: "1", AccessToken: "at",
	}); err != nil {
		t.Fatal(err)
	}
	var changed []string
	f.svc.OnIdentityChange = func(_ context.Context, userID string) { changed = append(changed, userID) }

	if err := f.svc.Unlink(context.Background(), "alice", "ident-1"); err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "alice" {
		t.Errorf("changed = %v, want [alice] after the unlink", changed)
	}
	// A failed unlink changed nothing and must not trigger a re-injection.
	if err := f.svc.Unlink(context.Background(), "alice", "ident-1"); err == nil {
		t.Fatal("second unlink should fail")
	}
	if len(changed) != 1 {
		t.Errorf("changed = %v, want a failed unlink to stay silent", changed)
	}
}

func TestSafeRedirect(t *testing.T) {
	cases := map[string]string{
		"/settings/accounts":        "/settings/accounts",
		"/a?tab=1":                  "/a?tab=1",
		"":                          "/",
		"//evil.test":               "/",
		"https://evil.test/":        "/",
		"http://evil.test/x":        "/",
		"/\\evil.test":              "/",
		"relative/path":             "/",
		"javascript:alert(1)":       "/",
		"/settings/accounts?ok=1#x": "/settings/accounts?ok=1",
	}
	for in, want := range cases {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}
