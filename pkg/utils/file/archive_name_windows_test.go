package file

import "testing"

func TestArchiveBaseNamesForWindowsRoots(t *testing.T) {
	for input, want := range map[string]string{`C:\`: "C-drive", `d:/`: "D-drive", `\\server\share\`: "share", `C:\Photos`: "Photos"} {
		if got := ArchiveBase(input); got != want {
			t.Fatal(input, got, want)
		}
	}
}
