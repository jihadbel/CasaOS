package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// Restrict the temporary file before writing private content into it.
func prepareAtomicWrite(path, temporary string) (bool, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return false, err
	}
	// Freeze the copied ACL on the staging file; ReplaceFileW retains the
	// destination's original ACL and inheritance when publishing the contents.
	err = windows.SetNamedSecurityInfo(temporary, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	return true, err
}

func extendedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(abs, `\\?\`) {
		return abs, nil
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(abs, `\\`), nil
	}
	return `\\?\` + abs, nil
}

func commitAtomicWrite(from, to string, existing bool) error {
	if !existing {
		return NoReplace(from, to)
	}
	source, err := extendedPath(from)
	if err != nil {
		return err
	}
	target, err := extendedPath(to)
	if err != nil {
		return err
	}
	src, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	// Never ignore metadata/ACL merge failures or fall back to a plain rename.
	for attempt := 0; ; attempt++ {
		ok, _, callErr := replaceFileW.Call(uintptr(unsafe.Pointer(dst)), uintptr(unsafe.Pointer(src)), 0, 0, 0, 0)
		if ok != 0 {
			return nil
		}
		// These failures leave both file names intact. Brief readers (including
		// Windows scanners) may hold a handle without FILE_SHARE_DELETE.
		transient := errors.Is(callErr, windows.ERROR_SHARING_VIOLATION) || errors.Is(callErr, windows.ERROR_LOCK_VIOLATION) || errors.Is(callErr, windows.ERROR_UNABLE_TO_REMOVE_REPLACED)
		if !transient || attempt >= 6 {
			return &os.LinkError{Op: "ReplaceFileW", Old: from, New: to, Err: callErr}
		}
		time.Sleep(time.Duration(1<<attempt) * 10 * time.Millisecond)
	}
}
