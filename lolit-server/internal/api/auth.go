package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lolit/lolit-server/internal/auth"
	"github.com/lolit/lolit-server/internal/db"
)

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	u, err := h.Auth.Authenticate(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	token, err := h.Auth.IssueToken(u)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Auth.SetSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": token, "user": u})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.Auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRegister creates a new account. Only usable, without authentication,
// while zero accounts exist (first-run bootstrap, becomes admin). After that
// it requires an authenticated admin caller to invite new members.
func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username required and password must be at least 8 characters"})
		return
	}

	count, err := h.Store.CountUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	role := db.RoleMember
	if count == 0 {
		// First account bootstraps as admin; anyone after that must be
		// invited by an existing admin via POST /api/users.
		role = db.RoleAdmin
	} else {
		id := auth.FromContext(r.Context())
		if id == nil || !id.IsAdmin() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only an admin can create new accounts"})
			return
		}
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	uid, err := h.Store.CreateUser(req.Username, hash, req.DisplayName, role, time.Now().Unix())
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}
	u, _ := h.Store.GetUserByID(uid)
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	u, err := h.Store.GetUserByID(id.UserID)
	if err != nil || u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user no longer exists"})
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// handleUsers lists members (GET) or removes one (DELETE ?id=). Admin only.
func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := h.Store.ListUsers()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, users)
	case http.MethodDelete:
		id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid id required"})
			return
		}
		caller := auth.FromContext(r.Context())
		if caller != nil && caller.UserID == id {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove your own account"})
			return
		}
		if err := h.Store.DeleteUser(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodPatch:
		var req struct {
			ID   int64  `json:"id"`
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.Role != db.RoleAdmin && req.Role != db.RoleMember {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be admin or member"})
			return
		}
		if err := h.Store.SetUserRole(req.ID, req.Role); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}
