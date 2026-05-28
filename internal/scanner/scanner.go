package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"context-steward/internal/config"
)

type ScanResult struct {
	Path        string
	FileType    string
	ContentHash string
	SizeBytes   int64
	ModifiedAt  time.Time
	Ignored     bool
}

// ScanWorkspace walks the configured workspace root directory recursively
func ScanWorkspace(cfg *config.Config) ([]ScanResult, error) {
	var results []ScanResult
	root := cfg.Workspace.Root

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path to workspace root
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		// Check if file or directory matches workspace ignore rules
		ignored := IsIgnored(relPath, cfg.Workspace.Ignore)

		if info.IsDir() {
			if ignored {
				return filepath.SkipDir
			}
			return nil
		}

		if ignored {
			results = append(results, ScanResult{
				Path:       relPath,
				Ignored:    true,
				SizeBytes:  info.Size(),
				ModifiedAt: info.ModTime(),
			})
			return nil
		}

		// Check if binary (skips binary files by default)
		binary, err := IsBinary(path)
		if err != nil {
			// Skip files we cannot read
			return nil
		}
		if binary {
			return nil
		}

		// Compute file hash
		hash, err := ComputeHash(path)
		if err != nil {
			return nil
		}

		// Extract file type (extension)
		fileType := strings.TrimPrefix(filepath.Ext(path), ".")
		if fileType == "" {
			fileType = "txt"
		}

		results = append(results, ScanResult{
			Path:        relPath,
			FileType:    fileType,
			ContentHash: hash,
			SizeBytes:   info.Size(),
			ModifiedAt:  info.ModTime(),
			Ignored:     false,
		})

		return nil
	})

	return results, err
}

// IsIgnored matches relative paths against configured ignore strings/directories
func IsIgnored(path string, ignoreList []string) bool {
	path = filepath.ToSlash(path)
	for _, pattern := range ignoreList {
		pattern = filepath.ToSlash(pattern)
		if pattern == "" {
			continue
		}

		// Directory patterns (ends with a slash)
		if strings.HasSuffix(pattern, "/") {
			dirPattern := strings.TrimSuffix(pattern, "/")
			parts := strings.Split(path, "/")
			for _, part := range parts {
				if part == dirPattern {
					return true
				}
			}
		} else {
			// Direct glob match or prefix match
			match, err := filepath.Match(pattern, path)
			if err == nil && match {
				return true
			}
			// Match base name
			matchBase, err := filepath.Match(pattern, filepath.Base(path))
			if err == nil && matchBase {
				return true
			}
			// Check contains (fallback for simple ignore substrings)
			if strings.Contains(path, pattern) {
				return true
			}
		}
	}
	return false
}

// IsBinary checks whether a file is a binary file based on its extension or null-byte detection
func IsBinary(path string) (bool, error) {
	// Fast check by extension to avoid unneeded disk IO
	ext := strings.ToLower(filepath.Ext(path))
	binaryExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".7z": true,
		".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
		".db": true, ".sqlite": true, ".sqlite3": true, ".mp3": true, ".mp4": true,
		".wav": true, ".avi": true, ".mov": true, ".dmg": true, ".iso": true,
		".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	}
	if binaryExts[ext] {
		return true, nil
	}

	// Read up to 1024 bytes and check for null byte
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	buf := make([]byte, 1024)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true, nil
		}
	}
	return false, nil
}

// ComputeHash calculates the SHA-256 hash of a file's content
func ComputeHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
