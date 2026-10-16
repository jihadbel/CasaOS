package file

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

// ValidateDeletePath rejects roots, internal data, their ancestors and links
// before any member of a deletion batch is removed.
func ValidateDeletePath(raw string) (string, error) {
	invalid := errors.New("cannot delete a storage root, internal system path or link")
	if !filepath.IsAbs(raw) {
		return "", invalid
	}
	clean := filepath.Clean(raw)
	if filepath.Dir(clean) == clean || InternalStoragePath(clean) {
		return "", invalid
	}
	for _, root := range []string{config.DefaultStorageRoot(), config.AppInfo.UserDataPath, config.AppInfo.DBPath, config.CommonInfo.RuntimePath, config.ConfigFilePath} {
		if root == "" {
			continue
		}
		root, err := filepath.Abs(root)
		if err != nil || strings.EqualFold(root, clean) || safefs.Within(clean, root) {
			return "", invalid
		}
	}
	if err := safefs.NoLinks(clean); err != nil {
		return "", invalid
	}
	return clean, nil
}
