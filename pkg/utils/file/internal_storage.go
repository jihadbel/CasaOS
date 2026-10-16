package file

import (
	"path/filepath"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

// InternalStoragePath is shared by every recursive file export.
func InternalStoragePath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	for _, root := range []string{config.AppInfo.UserDataPath, config.AppInfo.DBPath, config.CommonInfo.RuntimePath} {
		if root == "" {
			continue
		}
		internal, err := filepath.Abs(root)
		if err == nil && (strings.EqualFold(abs, internal) || safefs.Within(internal, abs)) {
			return true
		}
	}
	if config.ConfigFilePath != "" {
		configFile, err := filepath.Abs(config.ConfigFilePath)
		if err == nil && strings.EqualFold(abs, configFile) {
			return true
		}
	}
	bin := filepath.Join(config.DefaultStorageRoot(), "RecycleBin")
	for current := abs; ; current = filepath.Dir(current) {
		name := filepath.Base(current)
		if strings.EqualFold(name, ".temp") || strings.EqualFold(name, ".CasaOS-RecycleBin") || strings.EqualFold(name, "$RECYCLE.BIN") || strings.HasPrefix(strings.ToLower(name), ".casaos-") || strings.EqualFold(filepath.Clean(current), filepath.Clean(bin)) {
			return true
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}
