package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/lolit/lolit-server/internal/db"
	"github.com/lolit/lolit-server/internal/gitutil"
	"github.com/lolit/lolit-server/internal/search"
	"github.com/lolit/lolit-server/internal/ws"
)

// GiteaPayload is a minimal push webhook payload.
type GiteaPayload struct {
	Ref        string     `json:"ref"`
	Before     string     `json:"before"`
	After      string     `json:"after"`
	Repository Repository `json:"repository"`
	Commits    []Commit   `json:"commits"`
	Pusher     User       `json:"pusher"`
}

type Repository struct {
	FullName string `json:"full_name"`
	CloneURL string `json:"clone_url"`
}

type Commit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Author  User   `json:"author"`
	URL     string `json:"url"`
}

type User struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

type Handler struct {
	Store   *db.Store
	Search  *search.Engine
	Hub     *ws.Hub
	RepoRoot string
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var payload GiteaPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "json error", http.StatusBadRequest)
		return
	}
	repoName := payload.Repository.FullName
	if repoName == "" {
		http.Error(w, "missing repository", http.StatusBadRequest)
		return
	}

	// Save commits.
	for _, c := range payload.Commits {
		// Gitea timestamps are RFC3339 strings; approximate with 0 here for simplicity.
		if err := h.Store.SaveCommit(repoName, c.ID, c.Message, c.Author.Username, 0); err != nil {
			log.Println("save commit:", err)
		}
	}

	// Process changed files.
	repo := gitutil.NewRepo(h.RepoRoot, repoName)
	if !repo.Exists() {
		log.Printf("repo not found at %s, skipping metadata extraction", repo.Path)
		w.WriteHeader(http.StatusOK)
		return
	}

	base := payload.Before
	head := payload.After

	changes, err := repo.ChangedFiles(base, head)
	if err != nil {
		log.Println("changed files:", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, ch := range changes {
		ft := db.FileTypeFromPath(ch.Path)
		fileID, err := h.Store.UpsertFile(repoName, ch.Path, ft, head)
		if err != nil {
			log.Println("upsert file:", err)
			continue
		}

		// Index for search.
		content := ""
		if ft == "other" || ft == "kicad_pcb" || ft == "kicad_sch" {
			b, _ := repo.ShowFile(head, ch.Path)
			content = string(b)
		}
		docID := search.DocID(repoName, ch.Path)
		if err := h.Search.Index(docID, search.Doc{Repo: repoName, Path: ch.Path, Type: ft, Content: content}); err != nil {
			log.Println("index:", err)
		}

		// KiCAD diff (simplified: parse only top-level component list).
		if ft == "kicad_pcb" || ft == "kicad_sch" {
			if err := h.processKiCad(repo, fileID, repoName, ch.Path, base, head); err != nil {
				log.Println("kicad diff:", err)
			}
		}

		// STEP/STL previews would be generated here; placeholder.
		if ft == "step" || ft == "stl" {
			previewPath := filepath.Join("previews", fmt.Sprintf("%d_%s.png", fileID, head[:8]))
			_ = h.Store.SavePreview(fileID, head, previewPath)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) processKiCad(repo *gitutil.Repo, fileID int64, repoName, path, base, head string) error {
	oldData, _ := repo.ShowFile(base, path)
	newData, _ := repo.ShowFile(head, path)
	oldComps := parseComponents(string(oldData))
	newComps := parseComponents(string(newData))
	added, removed, changed := diffComponents(oldComps, newComps)
	diff := map[string]interface{}{
		"added":   added,
		"removed": removed,
		"changed": changed,
	}
	return h.Store.SaveKiCadDiff(fileID, head, diff)
}

type component struct {
	Ref        string `json:"ref"`
	Footprint  string `json:"footprint"`
	Value      string `json:"value"`
	NetSummary string `json:"net_summary"`
}

func parseComponents(data string) map[string]component {
	comps := make(map[string]component)
	// Very naive S-expression component extractor for MVP.
	// Matches (footprint ... (fp_text reference R1 ...) or (symbol ... (property "Reference" "R1" ...)).
	lines := strings.Split(data, "\n")
	var current *component
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "(module ") || strings.HasPrefix(line, "(symbol ") || strings.HasPrefix(line, "(footprint ") {
			current = &component{}
		}
		if current == nil {
			continue
		}
		if strings.Contains(line, "(fp_text reference ") || strings.Contains(line, "fp_text reference ") {
			parts := strings.Split(line, " ")
			for i, p := range parts {
				if p == "reference" && i+1 < len(parts) {
					current.Ref = strings.Trim(parts[i+1], "\"")
				}
			}
		}
		// Schematic symbols use (property "Reference" "R1" ...)
		if strings.Contains(line, "\"Reference\"") {
			parts := strings.Split(line, " ")
			for i, p := range parts {
				if p == "\"Reference\"" && i+1 < len(parts) {
					current.Ref = strings.Trim(parts[i+1], "\"")
				}
			}
		}
		if strings.Contains(line, "(footprint ") {
			start := strings.Index(line, "(footprint \"")
			if start >= 0 {
				rest := line[start+len("(footprint \""):]
				end := strings.Index(rest, "\"")
				if end >= 0 {
					current.Footprint = rest[:end]
				}
			}
		}
		if strings.Contains(line, "(value ") || strings.Contains(line, "fp_text value ") {
			start := strings.Index(line, "value \"")
			if start >= 0 {
				rest := line[start+len("value \""):]
				end := strings.Index(rest, "\"")
				if end >= 0 {
					current.Value = rest[:end]
				}
			}
		}
		if line == ")" && current != nil && current.Ref != "" {
			comps[current.Ref] = *current
			current = nil
		}
	}
	return comps
}

func diffComponents(old, new map[string]component) (added, removed, changed []component) {
	for ref, c := range new {
		if o, ok := old[ref]; !ok {
			added = append(added, c)
		} else if o != c {
			changed = append(changed, c)
		}
	}
	for ref, c := range old {
		if _, ok := new[ref]; !ok {
			removed = append(removed, c)
		}
	}
	return
}
