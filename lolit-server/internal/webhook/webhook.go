package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	ID        string `json:"id"`
	Message   string `json:"message"`
	Author    User   `json:"author"`
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"`
}

type User struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

type Handler struct {
	Store         *db.Store
	Search        *search.Engine
	Hub           *ws.Hub
	RepoRoot      string
	WebhookSecret string // if set, requests must carry a matching X-Gitea-Signature
}

// verifySignature checks the HMAC-SHA256 signature Gitea sends in the
// X-Gitea-Signature header (hex-encoded, computed over the raw body with the
// configured webhook secret). If no secret is configured, verification is
// skipped so the server keeps working for quick/local setups.
func (h *Handler) verifySignature(r *http.Request, body []byte) bool {
	if h.WebhookSecret == "" {
		return true
	}
	sig := r.Header.Get("X-Gitea-Signature")
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.WebhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
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
	if !h.verifySignature(r, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
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
	if !gitutil.IsValidRepoName(repoName) {
		http.Error(w, "invalid repository name", http.StatusBadRequest)
		return
	}

	// Save commits.
	for _, c := range payload.Commits {
		var ts int64
		if t, err := time.Parse(time.RFC3339, c.Timestamp); err == nil {
			ts = t.Unix()
		}
		if err := h.Store.SaveCommit(repoName, c.ID, c.Message, c.Author.Username, ts); err != nil {
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

// sexpNode is a minimal S-expression tree: either an atom (Text set, Children nil)
// or a list (Children set). It is intentionally lossy (comments/whitespace are
// dropped) but enough to walk KiCAD's footprint/symbol structure reliably,
// unlike a line-oriented scan which breaks whenever KiCAD reformats layout.
type sexpNode struct {
	Text     string
	Children []*sexpNode
}

// parseSexp parses a (possibly multi-line) S-expression document into a
// synthetic root node whose children are the top-level forms.
func parseSexp(data string) *sexpNode {
	root := &sexpNode{}
	stack := []*sexpNode{root}
	var buf strings.Builder
	inString := false

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		top := stack[len(stack)-1]
		top.Children = append(top.Children, &sexpNode{Text: buf.String()})
		buf.Reset()
	}

	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case inString:
			buf.WriteByte(c)
			if c == '\\' && i+1 < len(data) {
				buf.WriteByte(data[i+1])
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
		case c == '"':
			flush()
			buf.WriteByte(c)
			inString = true
		case c == '(':
			flush()
			node := &sexpNode{}
			stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, node)
			stack = append(stack, node)
		case c == ')':
			flush()
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			buf.WriteByte(c)
		}
	}
	flush()
	return root
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func (n *sexpNode) head() string {
	if len(n.Children) == 0 {
		return n.Text
	}
	return n.Children[0].Text
}

func (n *sexpNode) arg(i int) string {
	if i < len(n.Children) {
		return unquote(n.Children[i].Text)
	}
	return ""
}

// parseComponents extracts footprint/symbol components (keyed by reference
// designator) from a .kicad_pcb or .kicad_sch document.
func parseComponents(data string) map[string]component {
	comps := make(map[string]component)
	root := parseSexp(data)
	var walk func(n *sexpNode)
	walk = func(n *sexpNode) {
		if len(n.Children) == 0 {
			return
		}
		switch n.head() {
		case "module", "footprint", "symbol":
			if c, ok := extractComponent(n); ok {
				comps[c.Ref] = c
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, top := range root.Children {
		walk(top)
	}
	return comps
}

func extractComponent(n *sexpNode) (component, bool) {
	c := component{}
	if n.head() == "footprint" || n.head() == "module" {
		c.Footprint = n.arg(1)
	}
	nets := make(map[string]bool)
	var walk func(node *sexpNode)
	walk = func(node *sexpNode) {
		if len(node.Children) == 0 {
			return
		}
		switch node.head() {
		case "fp_text":
			if node.arg(1) == "reference" {
				c.Ref = node.arg(2)
			} else if node.arg(1) == "value" {
				c.Value = node.arg(2)
			}
		case "property":
			key := node.arg(1)
			if strings.EqualFold(key, "reference") {
				c.Ref = node.arg(2)
			} else if strings.EqualFold(key, "value") {
				c.Value = node.arg(2)
			}
		case "net":
			if name := node.arg(2); name != "" {
				nets[name] = true
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(n)
	netList := make([]string, 0, len(nets))
	for name := range nets {
		netList = append(netList, name)
	}
	sort.Strings(netList)
	c.NetSummary = strings.Join(netList, ",")
	return c, c.Ref != ""
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
