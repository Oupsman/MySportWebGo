package device

import "os"

// mountRoots returns the drives D: to Z:, A: and B: are floppies and C: the system
func mountRoots() ([]root, error) {
	var roots []root
	for letter := 'D'; letter <= 'Z'; letter++ {
		p := string(letter) + `:\`
		if _, err := os.Stat(p); err == nil {
			roots = append(roots, root{Path: p, ID: "path:" + p, Label: string(letter) + ":", Kind: MassStorage})
		}
	}
	return roots, nil
}
