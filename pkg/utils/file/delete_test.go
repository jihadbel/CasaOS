package file

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDeleteValidationRejectsFilesystemRootsWithoutMutation(t *testing.T) {
	// This test calls validation only; real drive roots never reach RemoveAll.
	roots := []string{filepath.VolumeName(t.TempDir()) + string(filepath.Separator)}
	if runtime.GOOS == "windows" {
		roots = append(roots, `C:\`, `D:\`, `C:`, `\\server\share\`, `\\?\C:\`)
	}
	for _, root := range roots {
		if _, err := ValidateDeletePath(root); err == nil {
			t.Fatal("accepted filesystem root", root)
		}
	}
}
