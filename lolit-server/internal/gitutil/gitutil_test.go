package gitutil

import "testing"

func TestIsValidRepoName(t *testing.T) {
	valid := []string{"team/robot2026", "a/b", "team/robot-2026_v2"}
	invalid := []string{"", "../etc/passwd", "team/../../../etc", "/etc/passwd", "team//repo", "team/"}

	for _, name := range valid {
		if !IsValidRepoName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}
	for _, name := range invalid {
		if IsValidRepoName(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestNewRepoRejectsPathTraversal(t *testing.T) {
	r := NewRepo("/var/lib/gitea/data/gitea-repositories", "../../etc/passwd")
	if r.Path != "" {
		t.Errorf("expected empty path for traversal attempt, got %q", r.Path)
	}
	if r.Exists() {
		t.Error("repo with empty path must never report as existing")
	}
}
