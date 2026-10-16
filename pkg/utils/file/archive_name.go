package file

import (
	"path/filepath"
	"strings"
)

// ArchiveBase gives drive and UNC roots a usable archive entry/file name.
func ArchiveBase(source string) string {
	clean := filepath.Clean(source)
	name := filepath.Base(clean)
	if IsFilesystemRoot(clean) {
		volume := filepath.VolumeName(clean)
		if len(volume) == 2 && volume[1] == ':' {
			return strings.ToUpper(volume[:1]) + "-drive"
		}
		volume = strings.TrimRight(volume, `\/`)
		name = volume[strings.LastIndexAny(volume, `\/`)+1:]
	}
	if name == "." || name == "" || strings.ContainsAny(name, `\/:`) {
		return "Archive"
	}
	return name
}

func IsFilesystemRoot(source string) bool {
	clean := filepath.Clean(source)
	if !filepath.IsAbs(clean) {
		return false
	}
	volume := filepath.VolumeName(clean)
	return filepath.Dir(clean) == clean || volume != "" && strings.EqualFold(strings.TrimRight(clean, `\/`), volume)
}
