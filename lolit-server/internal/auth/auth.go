// Package auth implements Lolit's own lightweight user accounts. These are
// deliberately separate from Gitea's user accounts: Gitea keeps owning raw
// git/LFS authentication and locking, while Lolit accounts authenticate the
// WebUI/API and attribute locks, uploads, and admin actions to a person.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/lolit/lolit-server/internal/db"
)

const sessionCookieName = "lolit_session"
const sessionDuration = 7 * 24 * time.Hour

// ErrInvalidCredentials is returned by Authenticate on a bad username/password.
var ErrInvalidCredentials = errors.New("invalid username or password")

type claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type Service struct {
	Store  *db.Store
	Secret []byte
}

func New(store *db.Store, secret string) *Service {
	return &Service{Store: store, Secret: []byte(secret)}
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// Authenticate verifies a username/password pair against Lolit's local user
// table and returns the matching user on success.
func (s *Service) Authenticate(username, password string) (*db.User, error) {
	u, err := s.Store.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

// IssueToken creates a signed session JWT for the given user.
func (s *Service) IssueToken(u *db.User) (string, error) {
	now := time.Now()
	c := claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(sessionDuration)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(s.Secret)
}

// SetSessionCookie attaches the session token as an HttpOnly cookie, used by
// the browser-based WebUI (the CLI instead sends it as a Bearer header).
func (s *Service) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// tokenFromRequest extracts a session token from the Authorization header
// (used by the rv CLI) or the session cookie (used by the browser WebUI).
func tokenFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value
	}
	return ""
}

// Identity is the authenticated caller attached to the request context.
type Identity struct {
	UserID   int64
	Username string
	Role     string
}

func (id Identity) IsAdmin() bool { return id.Role == db.RoleAdmin }

type contextKey int

const identityKey contextKey = 0

func (s *Service) verify(tokenStr string) (*Identity, error) {
	if tokenStr == "" {
		return nil, errors.New("no token")
	}
	tok, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (interface{}, error) {
		return s.Secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	c, ok := tok.Claims.(*claims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	return &Identity{UserID: c.UserID, Username: c.Username, Role: c.Role}, nil
}

// Middleware requires a valid session and attaches the caller's Identity to
// the request context. It responds 401 if authentication fails.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.verify(tokenFromRequest(r))
		if err != nil {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), identityKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalMiddleware attaches an Identity to the request context if a valid
// session is present, but never rejects the request. Used by endpoints that
// behave differently for anonymous vs authenticated callers (e.g. the
// first-run registration bootstrap).
func (s *Service) OptionalMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, err := s.verify(tokenFromRequest(r)); err == nil {
			r = r.WithContext(context.WithValue(r.Context(), identityKey, id))
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin wraps a handler that must only be reachable by admins. It
// assumes Middleware has already run and attached an Identity.
func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := FromContext(r.Context())
		if id == nil || !id.IsAdmin() {
			http.Error(w, `{"error":"admin role required"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey).(*Identity)
	return id
}
