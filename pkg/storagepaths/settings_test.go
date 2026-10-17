package storagepaths

import (
	"path/filepath"
	"testing"
)

func TestDedicatedDriveCanBeDefaultStorage(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	t.Setenv("CASAOS_DATA_ROOT", root)
	s := Default()
	if err := Validate(&s); err != nil {
		t.Fatal("dedicated filesystem root rejected", err)
	}
	s.PhotosPath = root
	if err := Validate(&s); err == nil {
		t.Fatal("category may not scan entire drive")
	}
}
