package file

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
)

// ValidateDeletePath rejects roots, internal data, their ancestors and links
// before any member of a deletion batch is removed.
func ValidateDeletePath(raw string) (string, error) {
	invalid := errors.New("cannot delete a storage root, internal system path or link")
	if !filepath.IsAbs(raw) {
		return "", invalid
	}
	clean, err := ValidateUserPath(raw)
	if err != nil {
		return "", err
	}
	if filepath.Dir(clean) == clean || InternalStoragePath(clean) {
		return "", invalid
	}
	settings, _, err := storagepaths.Read()
	if err != nil {
		return "", err
	}
	roots := append(settings.Roots(), config.DefaultStorageRoot(), config.AppInfo.UserDataPath, config.AppInfo.DBPath, config.CommonInfo.RuntimePath, config.ConfigFilePath)
	for _, root := range roots {
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
