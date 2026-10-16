package safefs

import "golang.org/x/sys/windows"

// NoReplace moves a file or directory on one volume without replacing a target.
func NoReplace(from, to string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFile(src, dst)
}
