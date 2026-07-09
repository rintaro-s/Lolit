package api

import (
	"testing"

	"github.com/lolit/lolit-server/internal/db"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	store, err := db.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &Handler{Store: store}
}

func TestResolveClosureWalksTransitiveDependencies(t *testing.T) {
	h := newTestHandler(t)
	const repo = "team/robot"

	// assembly.SLDASM @ v3 depends on sub.SLDASM @ v2 and bolt.SLDPRT @ v1
	must(t, h.Store.SaveDependencies(repo, "assembly.SLDASM", "v3", []db.Dependency{
		{DepPath: "sub.SLDASM", DepVersion: "v2"},
		{DepPath: "bolt.SLDPRT", DepVersion: "v1"},
	}))
	// sub.SLDASM @ v2 itself depends on nut.SLDPRT @ v1
	must(t, h.Store.SaveDependencies(repo, "sub.SLDASM", "v2", []db.Dependency{
		{DepPath: "nut.SLDPRT", DepVersion: "v1"},
	}))

	got, err := h.resolveClosure(repo, "assembly.SLDASM", "v3")
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}
	want := map[versionRef]bool{
		{"assembly.SLDASM", "v3"}: true,
		{"sub.SLDASM", "v2"}:      true,
		{"bolt.SLDPRT", "v1"}:     true,
		{"nut.SLDPRT", "v1"}:      true,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d files, got %d: %+v", len(want), len(got), got)
	}
	for _, ref := range got {
		if !want[ref] {
			t.Errorf("unexpected ref in closure: %+v", ref)
		}
	}
}

func TestResolveClosureToleratesCycles(t *testing.T) {
	h := newTestHandler(t)
	const repo = "team/robot"

	// a @ v1 depends on b @ v1, which (accidentally, or via a re-save loop)
	// depends back on a @ v1. Must terminate instead of infinite-looping.
	must(t, h.Store.SaveDependencies(repo, "a.SLDASM", "v1", []db.Dependency{
		{DepPath: "b.SLDASM", DepVersion: "v1"},
	}))
	must(t, h.Store.SaveDependencies(repo, "b.SLDASM", "v1", []db.Dependency{
		{DepPath: "a.SLDASM", DepVersion: "v1"},
	}))

	got, err := h.resolveClosure(repo, "a.SLDASM", "v1")
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 files in a 2-cycle, got %d: %+v", len(got), got)
	}
}

func TestResolveClosureLeafFileHasNoDependencies(t *testing.T) {
	h := newTestHandler(t)
	got, err := h.resolveClosure("team/robot", "readme.txt", "v1")
	if err != nil {
		t.Fatalf("resolveClosure: %v", err)
	}
	if len(got) != 1 || got[0] != (versionRef{"readme.txt", "v1"}) {
		t.Fatalf("expected just the file itself, got %+v", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
