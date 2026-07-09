package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lolit/lolit-server/internal/db"
	"github.com/lolit/lolit-server/internal/gitutil"
	"github.com/lolit/lolit-server/internal/search"
	"github.com/lolit/lolit-server/internal/ws"
)

type Handler struct {
	Store    *db.Store
	Search   *search.Engine
	Hub      *ws.Hub
	RepoRoot string
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/files", h.handleFiles)
	mux.HandleFunc("/api/file", h.handleFileDetail)
	mux.HandleFunc("/api/commits", h.handleCommits)
	mux.HandleFunc("/api/locks", h.handleLocks)
	mux.HandleFunc("/api/lock", h.handleLock)
	mux.HandleFunc("/api/search", h.handleSearch)
	mux.HandleFunc("/api/releases", h.handleReleases)
	mux.HandleFunc("/api/metadata", h.handleMetadata)
	mux.HandleFunc("/api/kicad-diff", h.handleKiCadDiff)
	mux.HandleFunc("/api/history", h.handleHistory)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) handleFiles(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo required"})
		return
	}
	prefix := r.URL.Query().Get("prefix")
	files, err := h.Store.ListFiles(repo, prefix)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (h *Handler) handleFileDetail(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	path := r.URL.Query().Get("path")
	if repo == "" || path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and path required"})
		return
	}
	f, err := h.Store.GetFile(repo, path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if f == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	swMeta, _ := h.Store.GetSWMetadata(f.ID, f.LatestCommit)
	kicadDiff, _ := h.Store.GetKiCadDiff(f.ID, f.LatestCommit)
	preview, _ := h.Store.GetPreview(f.ID, f.LatestCommit)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"file":        f,
		"sw_metadata": swMeta,
		"kicad_diff":  kicadDiff,
		"preview":     preview,
	})
}

func (h *Handler) handleCommits(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	commits, err := h.Store.RecentCommits(repo, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

func (h *Handler) handleLocks(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	locks, err := h.Store.ListLocks(repo)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, locks)
}

func (h *Handler) handleLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Repo string `json:"repo"`
		Path string `json:"path"`
		User string `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	lock := r.Method == http.MethodPost
	if err := h.Store.SetLock(req.Repo, req.Path, req.User, lock); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Hub.Broadcast(ws.Event{Type: "lock", Repo: req.Repo, Path: req.Path, User: req.User, Locked: lock})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q required"})
		return
	}
	res, err := h.Search.Search(q, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var hits []map[string]interface{}
	for _, hit := range res.Hits {
		hits = append(hits, map[string]interface{}{
			"id":     hit.ID,
			"score":  hit.Score,
			"fields": hit.Fields,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"total": res.Total, "hits": hits})
}

func (h *Handler) handleReleases(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo required"})
		return
	}
	if r.Method == http.MethodGet {
		rels, err := h.Store.ListReleases(repo)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rels)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Tag    string `json:"tag"`
			Commit string `json:"commit"`
			Note   string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := h.Store.CreateRelease(repo, req.Tag, req.Commit, req.Note, 0); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func (h *Handler) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Repo   string        `json:"repo"`
		Path   string        `json:"file"`
		Commit string        `json:"commit_hash"`
		Meta   db.SWMetadata `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	fileID, err := h.Store.UpsertFile(req.Repo, req.Path, db.FileTypeFromPath(req.Path), req.Commit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.Store.SaveSWMetadata(fileID, req.Commit, req.Meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleKiCadDiff accepts a component diff computed client-side (e.g. by the
// KiCAD plugin before a push) so it shows up immediately instead of waiting
// for the next Gitea webhook to trigger server-side diffing.
func (h *Handler) handleKiCadDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Repo   string      `json:"repo"`
		Path   string      `json:"path"`
		Commit string      `json:"commit"`
		Diff   interface{} `json:"diff"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Repo == "" || req.Path == "" || req.Commit == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo, path and commit required"})
		return
	}
	fileID, err := h.Store.UpsertFile(req.Repo, req.Path, db.FileTypeFromPath(req.Path), req.Commit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.Store.SaveKiCadDiff(fileID, req.Commit, req.Diff); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	repoName := r.URL.Query().Get("repo")
	path := r.URL.Query().Get("path")
	if repoName == "" || path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo and path required"})
		return
	}
	if !gitutil.IsValidRepoName(repoName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repo name"})
		return
	}
	repo := gitutil.NewRepo(h.RepoRoot, repoName)
	if !repo.Exists() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "repo not found"})
		return
	}
	commits, err := repo.FileLog(path, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

func (h *Handler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	locks, _ := h.Store.ListLocks("")
	commits, _ := h.Store.RecentCommits("", 5)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"lock_count":     len(locks),
		"recent_commits": commits,
	})
}
