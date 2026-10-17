package file

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
)

// ValidateStoragePath permits configured user storage, excluding internal data,
// filesystem roots and redirected path components, including missing targets.
func ValidateStoragePath(raw string) (string, error) {
	invalid := errors.New("path must be inside configured user storage and cannot be an internal path or link")
	if !filepath.IsAbs(raw) {
		return "", invalid
	}
	clean := filepath.Clean(raw)
	if filepath.Dir(clean) == clean || InternalStoragePath(clean) {
		return "", invalid
	}
	if runtime.GOOS == "windows" && strings.Contains(strings.TrimPrefix(clean, filepath.VolumeName(clean)), ":") {
		return "", invalid
	}
	settings, _, err := storagepaths.Read()
	if err != nil {
		return "", err
	}
	for _, root := range append(settings.Roots(), config.DefaultStorageRoot()) {
		root = filepath.Clean(root)
		equal := root == clean
		if runtime.GOOS == "windows" {
			equal = strings.EqualFold(root, clean)
		}
		if equal || safefs.Within(root, clean) {
			if err := safefs.NoLinks(clean); err != nil {
				return "", err
			}
			return clean, nil
		}
	}
	return "", invalid
}
