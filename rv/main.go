package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const version = "0.1.0"

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
	case "version", "--version", "-v":
		fmt.Println("rv", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`rv - Lolit CLI wrapper around Git / Git LFS

Usage:
  rv clone <repo>            # git clone wrapper (from Lolit Gitea)
  rv commit -m "message"     # git add . && git commit
  rv push                    # git push (with LFS)
  rv pull                    # git pull (with LFS)
  rv lock <file>             # git lfs lock <file>
  rv unlock <file>           # git lfs unlock <file>
  rv locks                   # list current locks
  rv history <file>          # git log --follow <file>
  rv search <query>          # search metadata server
  rv release <tag>           # create a release tag

Environment:
  LOLIT_SERVER    Metadata server URL (default http://localhost:8080)
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
		fmt.Fprintln(os.Stderr, "usage: rv clone <repo>")
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
		fmt.Fprintln(os.Stderr, "usage: rv commit -m \"message\"")
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
		fmt.Fprintf(os.Stderr, "usage: rv %s <file>\n", map[bool]string{true: "lock", false: "unlock"}[lock])
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
	// Notify metadata server.
	repo := currentRepoName()
	user, _ := gitOut("config", "user.name")
	if user == "" {
		user = os.Getenv("USER")
	}
	body, _ := json.Marshal(map[string]string{"repo": repo, "path": file, "user": user})
	method := map[bool]string{true: "POST", false: "DELETE"}[lock]
	_, _ = http.DefaultClient.Do(mustReq(method, "/api/lock", bytes.NewReader(body)))
}

func runLocks() {
	if err := git("lfs", "locks"); err != nil {
		os.Exit(1)
	}
}

func runHistory(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: rv history <file>")
		os.Exit(1)
	}
	if err := git("log", "--follow", args[0]); err != nil {
		os.Exit(1)
	}
}

func runSearch(args []string) {
	q := strings.Join(args, " ")
	if q == "" {
		fmt.Fprintln(os.Stderr, "usage: rv search <query>")
		os.Exit(1)
	}
	resp, err := http.Get(fmt.Sprintf("%s/api/search?q=%s", serverURL(), urlEnc(q)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "search error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	io.Copy(os.Stdout, resp.Body)
	fmt.Println()
}

func runRelease(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: rv release <tag>")
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
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/releases?repo=%s", serverURL(), urlEnc(repo)), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release register:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Println("released", tag)
}

func currentRepoName() string {
	if r := os.Getenv("LOLIT_REPO"); r != "" {
		return r
	}
	origin, _ := gitOut("remote", "get-url", "origin")
	origin = strings.TrimSuffix(origin, ".git")
	if i := strings.LastIndex(origin, "/"); i >= 0 {
		return origin[i+1:]
	}
	// fallback: owner/repo from path
	cwd, _ := os.Getwd()
	base := filepath.Base(cwd)
	return "team/" + base
}

func serverURL() string {
	return strings.TrimSuffix(getEnv("LOLIT_SERVER", "http://localhost:8080"), "/")
}

func mustReq(method, path string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, serverURL()+path, body)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "rv/"+version)
	return req
}

func urlEnc(s string) string {
	// Minimal URL encoding for query strings.
	return strings.NewReplacer(" ", "%20", "&", "%26", "=", "%3D", "?", "%3F").Replace(s)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// init ensures timestamp.
var _ = time.Now()
