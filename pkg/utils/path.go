package utils

import (
	"errors"
	"net/url"
	stdpath "path"
	"path/filepath"
	"runtime"
	"strings"
)

func isWindowsDrivePath(path string) bool {
	if len(path) >= 2 && path[1] == ':' {
		c := path[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//")
}

// FixAndCleanPath
// The upper layer of the root directory is still the root directory.
// So ".." And "." will be cleared
// for example
// 1. ".." or "." => "/"
// 2. "../..." or "./..." => "/..."
// 3. "../.x." or "./.x." => "/.x."
// 4. "x//\\y" = > "/z/x"
// On Windows: respects drive letters (e.g. C:/CasaOS/DATA) and UNC paths.
func FixAndCleanPath(path string) string {
	if runtime.GOOS == "windows" {
		if isWindowsDrivePath(path) || isUNCPath(path) {
			clean := filepath.Clean(path)
			return filepath.ToSlash(clean)
		}
	}
	path = strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return stdpath.Clean(path)
}

// PathAddSeparatorSuffix Add path '/' suffix
// for example /root => /root/
func PathAddSeparatorSuffix(path string) string {
	if !strings.HasSuffix(path, "/") {
		path = path + "/"
	}
	return path
}

// PathEqual judge path is equal
func PathEqual(path1, path2 string) bool {
	return FixAndCleanPath(path1) == FixAndCleanPath(path2)
}

func IsSubPath(path string, subPath string) bool {
	path, subPath = FixAndCleanPath(path), FixAndCleanPath(subPath)
	return path == subPath || strings.HasPrefix(subPath, PathAddSeparatorSuffix(path))
}

func Ext(path string) string {
	ext := stdpath.Ext(path)
	if strings.HasPrefix(ext, ".") {
		return ext[1:]
	}
	return ext
}

func EncodePath(path string, all ...bool) string {
	seg := strings.Split(path, "/")
	toReplace := []struct {
		Src string
		Dst string
	}{
		{Src: "%", Dst: "%25"},
		{"%", "%25"},
		{"?", "%3F"},
		{"#", "%23"},
	}
	for i := range seg {
		if len(all) > 0 && all[0] {
			seg[i] = url.PathEscape(seg[i])
		} else {
			for j := range toReplace {
				seg[i] = strings.ReplaceAll(seg[i], toReplace[j].Src, toReplace[j].Dst)
			}
		}
	}
	return strings.Join(seg, "/")
}

func JoinBasePath(basePath, reqPath string) (string, error) {
	if strings.HasSuffix(reqPath, "..") || strings.Contains(reqPath, "../") || strings.Contains(reqPath, `..\`) {
		return "", errors.New("access using relative path is not allowed")
	}
	if runtime.GOOS == "windows" && (isWindowsDrivePath(basePath) || isUNCPath(basePath)) {
		cleanBase := FixAndCleanPath(basePath)
		cleanReq := strings.TrimPrefix(FixAndCleanPath(reqPath), "/")
		return filepath.ToSlash(filepath.Join(cleanBase, cleanReq)), nil
	}
	return stdpath.Join(FixAndCleanPath(basePath), FixAndCleanPath(reqPath)), nil
}
