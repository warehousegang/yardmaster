package dashboard

import (
	"os"
	"path/filepath"
)

func findLogoPath() string {
	candidates := []string{
		filepath.Join("assets", "yardmaster-logo.png"),
		filepath.Join("assets", "yardmaster.png"),
		filepath.Join(os.Getenv("HOME"), "Downloads", "yardmaster.png"),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
