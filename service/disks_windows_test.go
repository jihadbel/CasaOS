package service

import (
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsDriveEnumerationSkipsAbsentOpticalAndRemoteDevices(t *testing.T) {
	var queried []string
	roots := localDriveRoots((1<<2)|(1<<3)|(1<<4)|(1<<5), func(root string) uint32 {
		queried = append(queried, root)
		switch root {
		case `C:\`:
			return windows.DRIVE_FIXED
		case `D:\`:
			return windows.DRIVE_CDROM
		case `E:\`:
			return windows.DRIVE_REMOVABLE
		default:
			return windows.DRIVE_REMOTE
		}
	})
	if len(queried) != 4 || !reflect.DeepEqual(roots, []string{`C:\`, `E:\`}) {
		t.Fatal(queried, roots)
	}
}
