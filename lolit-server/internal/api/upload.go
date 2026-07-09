package api

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lolit/lolit-server/internal/auth"
	"github.com/lolit/lolit-server/internal/gitutil"
)

const maxUploadMemory = 32 << 20 // 32MB kept in memory; larger parts spill to temp files automatically.

// handleUpload lets people who don't know Git drop one or more files (or a
// whole folder, via webkitdirectory/relative paths) straight into a repo
// from the browser. It stages the files in a throwaway shallow clone,
// commits as the calling user, and pushes through Gitea's normal git HTTP
// endpoint -- so Gitea's push webhook fires exactly as it would for a `git
// push`, and the existing metadata/diff/search pipeline runs unmodified.
func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	id := auth.FromContext(r.Context())
	if id == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	if h.GiteaUser == "" || h.GiteaPass == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "upload is not configured: server is missing Gitea admin credentials (LOLIT_GITEA_USER/LOLIT_GITEA_PASS)"})
		return
	}

	if err := r.ParseMultipartForm(maxUploadMemory); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not parse upload: " + err.Error()})
		return
	}
	repo := r.FormValue("repo")
	if !gitutil.IsValidRepoName(repo) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or missing repo"})
		return
	}
	message := strings.TrimSpace(r.FormValue("message"))

	files := r.MultipartForm.File["files"]
	paths := r.MultipartForm.Value["paths"]
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files uploaded"})
		return
	}
	if len(paths) != len(files) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "paths and files count mismatch"})
		return
	}
	cleanPaths := make([]string, len(paths))
	for i, p := range paths {
		clean, err := sanitizeUploadPath(p)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid path %q: %v", p, err)})
			return
		}
		cleanPaths[i] = clean
	}
	if message == "" {
		if len(cleanPaths) == 1 {
			message = "Upload " + cleanPaths[0]
		} else {
			message = fmt.Sprintf("Upload %d files", len(cleanPaths))
		}
	}

	// Refuse to overwrite files someone else is currently editing.
	for _, p := range cleanPaths {
		f, err := h.Store.GetFile(repo, p)
		if err == nil && f != nil && f.LockedBy != "" && f.LockedBy != id.Username {
			writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("%s is locked by %s", p, f.LockedBy)})
			return
		}
	}

	commit, err := h.pushUpload(r.Context(), repo, id, message, files, cleanPaths)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"commit": commit, "message": message})
}

// sanitizeUploadPath rejects absolute paths, empty segments and ".." so an
// uploaded file can never land outside the repo working tree.
func sanitizeUploadPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("path escapes repo root")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	return clean, nil
}

func (h *Handler) pushUpload(ctx context.Context, repo string, id *auth.Identity, message string, files []*multipart.FileHeader, paths []string) (string, error) {
	uploadsRoot := filepath.Join(h.DataDir, "tmp-uploads")
	if err := os.MkdirAll(uploadsRoot, 0o755); err != nil {
		return "", fmt.Errorf("create uploads dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(uploadsRoot, "upload-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneURL, err := authenticatedCloneURL(h.GiteaURL, h.GiteaUser, h.GiteaPass, repo)
	if err != nil {
		return "", err
	}

	run := func(name string, args ...string) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, name, args...)
		cmd.Dir = tmpDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("%s %v: %w (%s)", name, args, err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}

	if _, err := exec.Command("git", "clone", "--depth", "1", cloneURL, tmpDir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("clone repo (check the repo exists in Gitea and Gitea admin credentials are correct): %w", err)
	}
	_, _ = run("git", "lfs", "install", "--local") // best-effort; fine if git-lfs isn't installed

	for i, fh := range files {
		dest := filepath.Join(tmpDir, filepath.FromSlash(paths[i]))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", fmt.Errorf("create directory for %s: %w", paths[i], err)
		}
		if err := writeUploadedFile(fh, dest); err != nil {
			return "", fmt.Errorf("write %s: %w", paths[i], err)
		}
	}

	if _, err := run("git", "add", "-A"); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	if out, _ := run("git", "status", "--porcelain"); strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no changes to commit (uploaded content is identical to what's already there)")
	}

	authorEmail := id.Username + "@lolit.local"
	if _, err := run("git",
		"-c", "user.name="+id.Username,
		"-c", "user.email="+authorEmail,
		"commit", "-m", message); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	branch, err := run("git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if _, err := run("git", "push", "origin", "HEAD:"+branch); err != nil {
		return "", fmt.Errorf("git push: %w", err)
	}
	hash, err := run("git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve commit: %w", err)
	}
	return strings.TrimSpace(hash), nil
}

// authenticatedCloneURL builds a Gitea clone URL with HTTP basic auth
// credentials embedded, so the server can push on the uploading user's
// behalf without that user needing their own Gitea credentials configured
// locally. The commit author is still set to the Lolit user, so history
// stays attributed correctly even though the transport uses a shared
// service account.
func authenticatedCloneURL(giteaURL, user, pass, repo string) (string, error) {
	u, err := url.Parse(giteaURL)
	if err != nil {
		return "", fmt.Errorf("invalid LOLIT_GITEA_URL: %w", err)
	}
	u.User = url.UserPassword(user, pass)
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + repo + ".git"
	return u.String(), nil
}

func writeUploadedFile(fh *multipart.FileHeader, dest string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}
