package gitutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo provides git operations on a bare repository.
type Repo struct {
	Path string
}

// IsValidRepoName reports whether name is safe to use as a repo path segment,
// i.e. it cannot escape ReposRoot via "..", absolute paths, or empty segments.
func IsValidRepoName(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" {
			return false
		}
	}
	return !filepath.IsAbs(name)
}

func NewRepo(reposRoot, fullName string) *Repo {
	// fullName like "owner/repo"
	if !IsValidRepoName(fullName) {
		return &Repo{Path: ""}
	}
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return &Repo{Path: filepath.Join(reposRoot, fullName+".git")}
	}
	return &Repo{Path: filepath.Join(reposRoot, parts[0], parts[1]+".git")}
}

func (r *Repo) Exists() bool {
	if r.Path == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(r.Path, "HEAD"))
	return err == nil
}

func (r *Repo) git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Path, "-c", "safe.bareRepository=all"}, args...)...)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %v: %w (stderr: %s)", args, err, errBuf.String())
	}
	return out.Bytes(), nil
}

// ChangedFiles returns list of changed files with status between two commits.
// If base is an all-zero SHA, compares head against the empty tree.
func (r *Repo) ChangedFiles(base, head string) ([]FileChange, error) {
	isEmptyBase := strings.Trim(base, "0") == ""
	var out []byte
	var err error
	if isEmptyBase {
		out, err = r.git("diff-tree", "--no-commit-id", "--root", "--name-status", "-r", head)
	} else {
		out, err = r.git("diff", "--name-status", base+".."+head)
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var changes []FileChange
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[1]
		oldPath := ""
		if len(parts) == 3 {
			oldPath = parts[1]
			path = parts[2]
		}
		changes = append(changes, FileChange{Status: status, Path: path, OldPath: oldPath})
	}
	return changes, nil
}

func (r *Repo) ShowFile(commit, path string) ([]byte, error) {
	return r.git("show", commit+":"+path)
}

func (r *Repo) Log(limit int) ([]Commit, error) {
	format := "%H%x09%an%x09%at%x09%s"
	out, err := r.git("log", "-"+fmt.Sprint(limit), fmt.Sprintf("--pretty=format:%s", format))
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		var ts int64
		fmt.Sscanf(parts[2], "%d", &ts)
		commits = append(commits, Commit{Hash: parts[0], Author: parts[1], TS: ts, Message: parts[3]})
	}
	return commits, nil
}

func (r *Repo) FileLog(path string, limit int) ([]Commit, error) {
	format := "%H%x09%an%x09%at%x09%s"
	out, err := r.git("log", "-"+fmt.Sprint(limit), fmt.Sprintf("--pretty=format:%s", format), "--follow", "--", path)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		var ts int64
		fmt.Sscanf(parts[2], "%d", &ts)
		commits = append(commits, Commit{Hash: parts[0], Author: parts[1], TS: ts, Message: parts[3]})
	}
	return commits, nil
}

func (r *Repo) Head() (string, error) {
	out, err := r.git("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type FileChange struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
}

type Commit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	TS      int64  `json:"ts"`
	Message string `json:"message"`
}
