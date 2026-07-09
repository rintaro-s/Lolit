package db

import "testing"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSaveAndGetDependencies(t *testing.T) {
	s := newTestStore(t)
	deps := []Dependency{
		{DepPath: "parts/arm_link1.SLDPRT", DepVersion: "v1"},
		{DepPath: "parts/bolt.SLDPRT", DepVersion: "v2"},
	}
	if err := s.SaveDependencies("team/robot", "assembly.SLDASM", "v3", deps); err != nil {
		t.Fatalf("save dependencies: %v", err)
	}
	got, err := s.GetDependencies("team/robot", "assembly.SLDASM", "v3")
	if err != nil {
		t.Fatalf("get dependencies: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 dependencies, got %d: %+v", len(got), got)
	}
	for _, d := range got {
		if d.Path != "assembly.SLDASM" || d.Version != "v3" {
			t.Errorf("unexpected path/version on edge: %+v", d)
		}
	}

	// Re-saving the same (repo, path, version) must fully replace the edge
	// list, not accumulate duplicates -- otherwise a re-saved assembly would
	// leak stale dependency edges into every future bundle resolution.
	if err := s.SaveDependencies("team/robot", "assembly.SLDASM", "v3", []Dependency{
		{DepPath: "parts/arm_link1.SLDPRT", DepVersion: "v4"},
	}); err != nil {
		t.Fatalf("re-save dependencies: %v", err)
	}
	got, err = s.GetDependencies("team/robot", "assembly.SLDASM", "v3")
	if err != nil {
		t.Fatalf("get dependencies after re-save: %v", err)
	}
	if len(got) != 1 || got[0].DepVersion != "v4" {
		t.Fatalf("expected re-save to replace edges, got %+v", got)
	}

	none, err := s.GetDependencies("team/robot", "assembly.SLDASM", "v999")
	if err != nil {
		t.Fatalf("get dependencies for unknown version: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected no dependencies for unrecorded version, got %+v", none)
	}
}

func TestFileTypeFromPath(t *testing.T) {
	cases := map[string]string{
		"robot_arm.SLDASM":     "sldasm",
		"bolt.SLDPRT":          "sldprt",
		"bracket.sldprt":       "sldprt",
		"board.kicad_pcb":      "kicad_pcb",
		"sheet.kicad_sch":      "kicad_sch",
		"housing.STEP":         "step",
		"housing.stp":          "step",
		"housing.STL":          "stl",
		"README.md":            "other",
		"no_extension":         "other",
		"dir.sldprt/inner.txt": "other",
	}
	for path, want := range cases {
		if got := FileTypeFromPath(path); got != want {
			t.Errorf("FileTypeFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}
