//go:build !windows

package privatefs

import (
	"fmt"
	"os"
	"syscall"
)

func checkTrusted(path string, ancestor bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("untrusted service storage owner at %s; review before migration", path)
	}
	// A root-owned sticky ancestor (such as /tmp) cannot replace root-owned children.
	if info.Mode().Perm()&0022 != 0 && !(ancestor && info.IsDir() && info.Mode()&os.ModeSticky != 0) {
		return fmt.Errorf("untrusted users can write service storage at %s; review before migration", path)
	}
	return nil
}

func createPrivateDirectory(path string) error { return os.Mkdir(path, 0700) }
