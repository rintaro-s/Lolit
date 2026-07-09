package db

import "testing"

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
