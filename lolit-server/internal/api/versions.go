package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/lolit/lolit-server/internal/db"
	"github.com/lolit/lolit-server/internal/gitutil"
)

// fetchFileContent returns the real bytes of repo/path as of version,
// transparently resolving Git LFS pointers so callers never see pointer
// text for CAD binaries. "version" is Lolit's user-facing name for what is,
// internally, a git commit hash -- an implementation detail callers of this
// API don't need to know about.
func (h *Handler) fetchFileContent(repoName, path, version string) ([]byte, error) {
	if !gitutil.IsValidRepoName(repoName) {
		return nil, fmt.Errorf("invalid repo")
	}
	repo := gitutil.NewRepo(h.RepoRoot, repoName)
	if !repo.Exists() {
		return nil, fmt.Errorf("repo not found")
	}
	content, err := repo.ShowFile(version, path)
	if err != nil {
		return nil, fmt.Errorf("version %s of %s not found", version, path)
	}
	if !gitutil.IsLFSPointer(content) {
		return content, nil
	}
	if h.GiteaUser == "" || h.GiteaPass == "" {
		return nil, fmt.Errorf("this file is stored externally and the server has no Gitea credentials configured to fetch it")
	}
	oid, size, err := gitutil.ParseLFSPointer(content)
	if err != nil {
		return nil, err
	}
	return gitutil.FetchLFSObject(h.GiteaURL, h.GiteaUser, h.GiteaPass, repoName, oid, size)
}

// handleDownload streams a single file's content as of a given version.
func (h *Handler) handleDownload(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	path := r.URL.Query().Get("path")
	version := r.URL.Query().Get("version")
	if repo == "" || path == "" || version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo, path and version required"})
		return
	}
	content, err := h.fetchFileContent(repo, path, version)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filepath.Base(path)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(content)
}

// versionRef is a (path, version) pair as used by the resolve/bundle
// endpoints -- "version" here is what SaveDependencies/GetDependencies call
// dep_path/dep_version internally.
type versionRef struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// resolveClosure walks the dependency graph recorded for (path, version)
// and returns the full set of (path, version) pairs needed to open it --
// itself plus every direct and transitive dependency, each pinned to the
// exact version that was in effect when the referencing file was saved.
func (h *Handler) resolveClosure(repo, path, version string) ([]versionRef, error) {
	visited := map[versionRef]bool{}
	var order []versionRef
	var walk func(p, v string) error
	walk = func(p, v string) error {
		key := versionRef{p, v}
		if visited[key] {
			return nil
		}
		visited[key] = true
		order = append(order, key)
		deps, err := h.Store.GetDependencies(repo, p, v)
		if err != nil {
			return err
		}
		for _, d := range deps {
			if err := walk(d.DepPath, d.DepVersion); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(path, version); err != nil {
		return nil, err
	}
	return order, nil
}

// handleDependencies lets a client (currently the SolidWorks Add-in) record
// the reference graph for an assembly/sub-assembly as of the version it was
// just saved at. Only the client can read this -- the server has no
// SolidWorks API access.
func (h *Handler) handleDependencies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Repo    string `json:"repo"`
		Path    string `json:"path"`
		Version string `json:"version"`
		Deps    []struct {
			Path    string `json:"path"`
			Version string `json:"version"`
		} `json:"deps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Repo == "" || req.Path == "" || req.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo, path and version required"})
		return
	}
	deps := make([]db.Dependency, len(req.Deps))
	for i, d := range req.Deps {
		deps[i] = db.Dependency{DepPath: d.Path, DepVersion: d.Version}
	}
	if err := h.Store.SaveDependencies(req.Repo, req.Path, req.Version, deps); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleResolve returns the full set of files (each pinned to its own
// version) needed to open repo/path as of version -- what the WebUI and the
// SolidWorks Add-in use to preview "what would downloading this bundle
// actually include" before committing to it.
func (h *Handler) handleResolve(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	path := r.URL.Query().Get("path")
	version := r.URL.Query().Get("version")
	if repo == "" || path == "" || version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo, path and version required"})
		return
	}
	files, err := h.resolveClosure(repo, path, version)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": files})
}

// handleDownloadBundle streams a zip of repo/path as of version together
// with every dependency, each at its own pinned version, laid out at their
// repo-relative paths so a CAD assembly's references still resolve after
// extracting the zip into an empty folder.
func (h *Handler) handleDownloadBundle(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	path := r.URL.Query().Get("path")
	version := r.URL.Query().Get("version")
	if repo == "" || path == "" || version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo, path and version required"})
		return
	}
	files, err := h.resolveClosure(repo, path, version)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filepath.Base(path)+"-bundle.zip"))
	w.Header().Set("Content-Type", "application/zip")
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, f := range files {
		content, err := h.fetchFileContent(repo, f.Path, f.Version)
		if err != nil {
			// Best-effort: note the miss inside the zip rather than aborting
			// a bundle that's otherwise mostly usable.
			if entry, zerr := zw.Create(f.Path + ".MISSING.txt"); zerr == nil {
				fmt.Fprintf(entry, "could not fetch %s @ %s: %v", f.Path, f.Version, err)
			}
			continue
		}
		entry, err := zw.Create(f.Path)
		if err != nil {
			continue
		}
		entry.Write(content)
	}
}
