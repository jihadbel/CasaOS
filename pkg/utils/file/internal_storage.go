package file

import (
	"path/filepath"
	"strings"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

// InternalStoragePath is shared by every recursive file export.
func InternalStoragePath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	bin := filepath.Join(config.DefaultStorageRoot(), "RecycleBin")
	for current := abs; ; current = filepath.Dir(current) {
		name := filepath.Base(current)
		if name == ".temp" || strings.EqualFold(name, ".CasaOS-RecycleBin") || strings.EqualFold(name, "$RECYCLE.BIN") || strings.HasPrefix(strings.ToLower(name), ".casaos-") || strings.EqualFold(filepath.Clean(current), filepath.Clean(bin)) {
			return true
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}
