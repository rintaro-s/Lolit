package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// handleRepos lists repositories Lolit can see under RepoRoot, discovered
// directly from Gitea's bare-repo layout ("<owner>/<repo>.git"), so the
// WebUI can offer a repo switcher instead of requiring users to know and
// type an exact "owner/repo" string.
func (h *Handler) handleRepos(w http.ResponseWriter, r *http.Request) {
	repos := []string{}
	owners, err := os.ReadDir(h.RepoRoot)
	if err != nil {
		writeJSON(w, http.StatusOK, repos)
		return
	}
	for _, owner := range owners {
		if !owner.IsDir() {
			continue
		}
		ownerPath := filepath.Join(h.RepoRoot, owner.Name())
		entries, err := os.ReadDir(ownerPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasSuffix(e.Name(), ".git") {
				continue
			}
			repos = append(repos, owner.Name()+"/"+strings.TrimSuffix(e.Name(), ".git"))
		}
	}
	sort.Strings(repos)
	writeJSON(w, http.StatusOK, repos)
}
