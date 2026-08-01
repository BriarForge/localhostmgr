// homePath is a tiny helper used by the launchd plist template.
// Lives in a separate file to avoid dragging the templating into tests.
package launchd

import (
	"os"
	"path/filepath"
)

func homePath(rel string) string {
	h, err := os.UserHomeDir()
	if err != nil {
		return rel
	}
	return filepath.Join(h, rel)
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/Users/mike"
	}
	return h
}
