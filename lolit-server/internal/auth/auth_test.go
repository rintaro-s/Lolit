package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lolit/lolit-server/internal/db"
)

func newTestService(t *testing.T) (*Service, *db.Store) {
	t.Helper()
	store, err := db.New(filepathTempDB(t))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return New(store, "test-secret"), store
}

func filepathTempDB(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/test.db"
}

func TestAuthenticateSuccessAndFailure(t *testing.T) {
	svc, store := newTestService(t)
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := store.CreateUser("alice", hash, "Alice", db.RoleMember, time.Now().Unix()); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := svc.Authenticate("alice", "wrong password"); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for wrong password, got %v", err)
	}
	if _, err := svc.Authenticate("bob", "anything"); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for unknown user, got %v", err)
	}
	u, err := svc.Authenticate("alice", "correct horse battery staple")
	if err != nil {
		t.Fatalf("expected successful authentication, got %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("unexpected user: %+v", u)
	}
}

func TestIssueTokenAndMiddleware(t *testing.T) {
	svc, store := newTestService(t)
	hash, _ := HashPassword("password123")
	uid, _ := store.CreateUser("carol", hash, "Carol", db.RoleAdmin, time.Now().Unix())
	u, _ := store.GetUserByID(uid)

	token, err := svc.IssueToken(u)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	var gotIdentity *Identity
	handler := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdentity = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// No credentials -> 401.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/files", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with no token, got %d", rec.Code)
	}

	// Bearer token -> passes through with the right identity.
	req := httptest.NewRequest("GET", "/api/files", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid bearer token, got %d", rec.Code)
	}
	if gotIdentity == nil || gotIdentity.Username != "carol" || !gotIdentity.IsAdmin() {
		t.Errorf("unexpected identity: %+v", gotIdentity)
	}

	// Garbage token -> 401.
	req = httptest.NewRequest("GET", "/api/files", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with garbage token, got %d", rec.Code)
	}
}

func TestOptionalMiddlewareNeverRejects(t *testing.T) {
	svc, _ := newTestService(t)
	var sawIdentity bool
	handler := svc.OptionalMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawIdentity = FromContext(r.Context()) != nil
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("POST", "/api/auth/register", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 even with no credentials, got %d", rec.Code)
	}
	if sawIdentity {
		t.Error("expected no identity to be attached with no credentials")
	}
}
