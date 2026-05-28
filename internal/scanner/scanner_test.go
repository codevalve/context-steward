package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsIgnored(t *testing.T) {
	ignores := []string{".git/", "node_modules/", "*.sqlite", "build/"}

	tests := []struct {
		path     string
		expected bool
	}{
		{"src/main.go", false},
		{".git/config", true},
		{"node_modules/lodash/index.js", true},
		{"index.sqlite", true},
		{"build/bin/app", true},
		{"docs/README.md", false},
	}

	for _, tc := range tests {
		result := IsIgnored(tc.path, ignores)
		if result != tc.expected {
			t.Errorf("IsIgnored(%q) = %v; expected %v", tc.path, result, tc.expected)
		}
	}
}

func TestIsBinary(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "csteward_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test text file
	txtFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(txtFile, []byte("Hello, world! This is a test text file."), 0644)
	if err != nil {
		t.Fatal(err)
	}

	binary, err := IsBinary(txtFile)
	if err != nil {
		t.Errorf("IsBinary failed on text file: %v", err)
	}
	if binary {
		t.Errorf("IsBinary(%q) returned true, expected false", txtFile)
	}

	// Test binary file (null bytes)
	binFile := filepath.Join(tmpDir, "test.bin")
	err = os.WriteFile(binFile, []byte{0x00, 0x01, 0x02, 0x03}, 0644)
	if err != nil {
		t.Fatal(err)
	}

	binary, err = IsBinary(binFile)
	if err != nil {
		t.Errorf("IsBinary failed on binary file: %v", err)
	}
	if !binary {
		t.Errorf("IsBinary(%q) returned false, expected true", binFile)
	}
}
