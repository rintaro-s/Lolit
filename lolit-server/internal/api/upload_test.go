package api

import "testing"

func TestSanitizeUploadPath(t *testing.T) {
	valid := map[string]string{
		"board.kicad_pcb":            "board.kicad_pcb",
		"electrical/board.kicad_pcb": "electrical/board.kicad_pcb",
		"/leading/slash.txt":         "leading/slash.txt",
		"a\\b\\c.txt":                "a/b/c.txt",
	}
	for in, want := range valid {
		got, err := sanitizeUploadPath(in)
		if err != nil {
			t.Errorf("sanitizeUploadPath(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("sanitizeUploadPath(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := []string{"", "..", "../etc/passwd", "a/../../b", "a/../../../b.txt"}
	for _, in := range invalid {
		if _, err := sanitizeUploadPath(in); err == nil {
			t.Errorf("sanitizeUploadPath(%q) expected error, got none", in)
		}
	}
}
