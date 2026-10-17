package file

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
)

// ValidateUserPath follows the file browser's administrator access policy.
// Internal service data, links, relative paths and NTFS streams are excluded.
func ValidateUserPath(raw string) (string, error) {
	if !filepath.IsAbs(raw) {
		return "", errors.New("an absolute user path is required")
	}
	clean := filepath.Clean(raw)
	if runtime.GOOS == "windows" && (strings.HasPrefix(clean, `\\?\`) || strings.HasPrefix(clean, `\\.\`) || strings.HasPrefix(clean, `\??\`)) {
		return "", errors.New("Windows device namespaces are not supported")
	}
	if filepath.Dir(clean) == clean || InternalStoragePath(clean) {
		return "", errors.New("filesystem roots and internal storage are protected")
	}
	if runtime.GOOS == "windows" && strings.Contains(strings.TrimPrefix(clean, filepath.VolumeName(clean)), ":") {
		return "", errors.New("NTFS alternate data streams are not supported")
	}
	return clean, safefs.NoLinks(clean)
}
