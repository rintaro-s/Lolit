package gitutil

import "testing"

func TestIsLFSPointer(t *testing.T) {
	pointer := []byte("version https://git-lfs.github.com/spec/v1\noid sha256:abc123\nsize 42\n")
	if !IsLFSPointer(pointer) {
		t.Error("expected pointer content to be detected as an LFS pointer")
	}
	if IsLFSPointer([]byte("just a normal text file\n")) {
		t.Error("expected normal content not to be detected as an LFS pointer")
	}
	if IsLFSPointer([]byte{0x00, 0x01, 0x02, 'P', 'K'}) {
		t.Error("expected binary content not to be detected as an LFS pointer")
	}
}

func TestParseLFSPointer(t *testing.T) {
	pointer := []byte("version https://git-lfs.github.com/spec/v1\noid sha256:4d7a214614ab2935\nsize 12345\n")
	oid, size, err := ParseLFSPointer(pointer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if oid != "4d7a214614ab2935" {
		t.Errorf("unexpected oid: %q", oid)
	}
	if size != 12345 {
		t.Errorf("unexpected size: %d", size)
	}

	if _, _, err := ParseLFSPointer([]byte("not a pointer")); err == nil {
		t.Error("expected error for non-pointer content")
	}
}
