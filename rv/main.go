package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const version = "0.1.0"

var httpClient = &http.Client{Timeout: 10 * time.Second}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "clone":
		runClone(args)
	case "commit":
		runCommit(args)
	case "push":
		runPush()
	case "pull":
		runPull()
	case "lock":
		runLock(args, true)
	case "unlock":
		runLock(args, false)
	case "locks":
		runLocks()
	case "history":
		runHistory(args)
	case "search":
		runSearch(args)
	case "release":
		runRelease(args)
	case "doctor":
		runDoctor()
	case "login":
		runLogin(args)
	case "logout":
		runLogout()
	case "whoami":
		runWhoami()
	case "version", "--version", "-v":
		fmt.Println("loli", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`loli - Lolit CLI wrapper around Git / Git LFS

Usage:
  loli clone <repo>            # git clone wrapper (from Lolit Gitea)
  loli commit -m "message"     # git add . && git commit
  loli push                    # git push (with LFS)
  loli pull                    # git pull (with LFS)
  loli lock <file>             # git lfs lock <file>
  loli unlock <file>           # git lfs unlock <file>
  loli locks                   # list current locks
  loli history <file>          # git log --follow <file>
  loli search <query>          # search metadata server
  loli release <tag>           # create a release tag
  loli doctor                  # check git/git-lfs and server connectivity
  loli login [username]        # log in to the Lolit metadata server
  loli logout                  # forget saved credentials
  loli whoami                  # show the currently logged-in user

Environment:
  LOLIT_SERVER    Metadata server URL (default http://localhost:8080)
  LOLIT_GITEA_URL Gitea URL (default http://localhost:3000)
  LOLIT_REPO      Repository full name for search/release (default auto)`)
}

func git(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func gitOut(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func runClone(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: loli clone <repo>")
		os.Exit(1)
	}
	repo := args[0]
	server := getEnv("LOLIT_GITEA_URL", "http://localhost:3000")
	url := fmt.Sprintf("%s/%s.git", server, repo)
	cloneArgs := []string{"clone", url}
	cloneArgs = append(cloneArgs, args[1:]...)
	if err := git(cloneArgs...); err != nil {
		os.Exit(1)
	}
}

func runCommit(args []string) {
	msg := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-m" && i+1 < len(args) {
			msg = args[i+1]
			i++
		}
	}
	if msg == "" {
		fmt.Fprintln(os.Stderr, "usage: loli commit -m \"message\"")
		os.Exit(1)
	}
	if err := git("add", "."); err != nil {
		os.Exit(1)
	}
	if err := git("commit", "-m", msg); err != nil {
		os.Exit(1)
	}
}

func runPush() {
	if err := git("lfs", "push", "origin", "--all"); err != nil {
		// ignore errors for bare remotes without lfs
	}
	if err := git("push"); err != nil {
		os.Exit(1)
	}
}

func runPull() {
	if err := git("pull"); err != nil {
		os.Exit(1)
	}
	if err := git("lfs", "pull"); err != nil {
		os.Exit(1)
	}
}

func runLock(args []string, lock bool) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: loli %s <file>\n", map[bool]string{true: "lock", false: "unlock"}[lock])
		os.Exit(1)
	}
	file := args[0]
	if lock {
		if err := git("lfs", "lock", file); err != nil {
			os.Exit(1)
		}
	} else {
		if err := git("lfs", "unlock", file); err != nil {
			os.Exit(1)
		}
	}
	// Notify the metadata server so the WebUI reflects the lock immediately.
	// This is best-effort: the Git LFS lock above is already authoritative,
	// so a server hiccup here should warn, not fail the command.
	repo := currentRepoName()
	user, _ := gitOut("config", "user.name")
	if user == "" {
		user = os.Getenv("USER")
	}
	body, _ := json.Marshal(map[string]string{"repo": repo, "path": file, "user": user})
	method := map[bool]string{true: "POST", false: "DELETE"}[lock]
	req, err := mustReq(method, "/api/lock", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not notify metadata server:", err)
		return
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not notify metadata server:", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Fprintln(os.Stderr, "warning: not logged in to the metadata server; run `loli login` so locks show up in the WebUI")
	} else if resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "warning: metadata server returned %s\n", resp.Status)
	}
}

func runLocks() {
	if err := git("lfs", "locks"); err != nil {
		os.Exit(1)
	}
}

func runHistory(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: loli history <file>")
		os.Exit(1)
	}
	if err := git("log", "--follow", args[0]); err != nil {
		os.Exit(1)
	}
}

func runSearch(args []string) {
	q := strings.Join(args, " ")
	if q == "" {
		fmt.Fprintln(os.Stderr, "usage: loli search <query>")
		os.Exit(1)
	}
	path := fmt.Sprintf("/api/search?%s", url.Values{"q": {q}}.Encode())
	req, err := mustReq("GET", path, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "search error:", err)
		os.Exit(1)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "search error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Fprintln(os.Stderr, "ログインしていません。`loli login` を実行してください。")
		os.Exit(1)
	}
	io.Copy(os.Stdout, resp.Body)
	fmt.Println()
}

func runRelease(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: loli release <tag>")
		os.Exit(1)
	}
	tag := args[0]
	// Create git tag.
	if err := git("tag", tag); err != nil {
		fmt.Fprintln(os.Stderr, "git tag:", err)
		os.Exit(1)
	}
	if err := git("push", "origin", tag); err != nil {
		fmt.Fprintln(os.Stderr, "git push tag:", err)
		os.Exit(1)
	}
	// Register release in metadata server.
	commit, _ := gitOut("rev-parse", tag)
	if commit == "" {
		commit, _ = gitOut("rev-parse", "HEAD")
	}
	repo := currentRepoName()
	body, _ := json.Marshal(map[string]string{"tag": tag, "commit": commit, "note": ""})
	path := fmt.Sprintf("/api/releases?%s", url.Values{"repo": {repo}}.Encode())
	req, err := mustReq("POST", path, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "release register:", err)
		os.Exit(1)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release register:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Fprintln(os.Stderr, "ログインしていません。`loli login` を実行してください。")
		os.Exit(1)
	}
	if resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "release register: server returned %s\n", resp.Status)
		os.Exit(1)
	}
	fmt.Println("released", tag)
}

// runDoctor checks the local toolchain and connectivity to Gitea/the Lolit
// metadata server, so non-technical teammates get an actionable diagnosis
// instead of a confusing failure deep inside some other command.
func runDoctor() {
	ok := true
	check := func(label string, err error, hint string) {
		if err != nil {
			ok = false
			fmt.Printf("[NG] %s: %v\n", label, err)
			if hint != "" {
				fmt.Printf("     -> %s\n", hint)
			}
			return
		}
		fmt.Printf("[OK] %s\n", label)
	}

	_, err := gitOut("--version")
	check("git installed", err, "install git and ensure it's on PATH")

	_, err = gitOut("lfs", "version")
	check("git-lfs installed", err, "install git-lfs: https://git-lfs.com")

	giteaURL := getEnv("LOLIT_GITEA_URL", "http://localhost:3000")
	_, err = httpGetOK(giteaURL)
	check(fmt.Sprintf("Gitea reachable (%s)", giteaURL), err, "check LOLIT_GITEA_URL and that Gitea is running")

	server := serverURL()
	_, err = httpGetOK(server + "/healthz")
	check(fmt.Sprintf("Lolit metadata server reachable (%s)", server), err, "check LOLIT_SERVER and that lolit-server is running")

	if authToken() == "" {
		check("logged in to Lolit", fmt.Errorf("not logged in"), "run `loli login`")
	} else {
		check("logged in to Lolit", nil, "")
	}

	if !ok {
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}

func httpGetOK(url string) (int, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return resp.StatusCode, fmt.Errorf("server error: %s", resp.Status)
	}
	return resp.StatusCode, nil
}

// currentRepoName returns the "owner/repo" full name Lolit uses to key
// metadata, preferring LOLIT_REPO, then the last two path segments of the
// origin remote, then a "team/<dir>" fallback.
func currentRepoName() string {
	if r := os.Getenv("LOLIT_REPO"); r != "" {
		return r
	}
	origin, _ := gitOut("remote", "get-url", "origin")
	if name := repoNameFromOriginURL(origin); name != "" {
		return name
	}
	// fallback: owner/repo from path
	cwd, _ := os.Getwd()
	base := filepath.Base(cwd)
	return "team/" + base
}

// repoNameFromOriginURL extracts "owner/repo" (the last two path segments)
// from a git remote URL such as "http://host:3000/team/robot2026.git" or
// "git@host:team/robot2026.git". Returns "" if it can't find two segments.
func repoNameFromOriginURL(origin string) string {
	origin = strings.TrimSuffix(strings.TrimSpace(origin), ".git")
	origin = strings.TrimSuffix(origin, "/")
	origin = strings.ReplaceAll(origin, ":", "/") // scp-like "git@host:owner/repo"
	if origin == "" {
		return ""
	}
	parts := strings.Split(origin, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func serverURL() string {
	return strings.TrimSuffix(getEnv("LOLIT_SERVER", "http://localhost:8080"), "/")
}

func mustReq(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, serverURL()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "loli/"+version)
	if token := authToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
