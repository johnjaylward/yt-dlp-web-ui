package safefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenDownloadRoot opens the configured download directory as a filesystem
// boundary. Root methods reject paths and symlinks that escape this directory.
func OpenDownloadRoot(path string) (*os.Root, error) {
	if path == "" {
		return nil, fmt.Errorf("download root is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(abs)
}

// RelativePath converts an absolute or relative path to a local path beneath
// root. It rejects traversal and paths on another volume.
func RelativePath(root, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pathAbs := path
	if !filepath.IsAbs(pathAbs) {
		pathAbs = filepath.Join(rootAbs, pathAbs)
	}
	pathAbs, err = filepath.Abs(pathAbs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return "", err
	}
	if rel != "." && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return "", fmt.Errorf("path is outside download root")
	}
	return rel, nil
}

// CleanRelativePath validates a path supplied as relative to the download root.
func CleanRelativePath(path string) (string, error) {
	if path == "" {
		return ".", nil
	}
	if filepath.IsAbs(path) || !filepath.IsLocal(path) {
		return "", fmt.Errorf("path must stay beneath download root")
	}
	clean := filepath.Clean(path)
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("path must stay beneath download root")
	}
	return clean, nil
}
