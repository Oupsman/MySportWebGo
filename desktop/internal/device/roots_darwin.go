package device

import (
	"os"
	"path/filepath"
)

// mountRoots returns the volumes of /Volumes, the boot volume included: it has no activity folder
func mountRoots() ([]root, error) {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil, err
	}
	var roots []root
	for _, entry := range entries {
		p := filepath.Join("/Volumes", entry.Name())
		roots = append(roots, root{Path: p, ID: "path:" + p, Label: entry.Name(), Kind: MassStorage})
	}
	return roots, nil
}
