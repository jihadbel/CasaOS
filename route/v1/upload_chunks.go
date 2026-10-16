package v1

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

func uploadTargetDir(rawPath, fileName, relative string) (string, error) {
	if rawPath == "" || rawPath == "/" {
		rawPath = config.DefaultStorageRoot()
	}
	if rawPath == "/DATA" || strings.HasPrefix(rawPath, "/DATA/") {
		rawPath = filepath.Join(config.DefaultStorageRoot(), strings.TrimLeft(strings.TrimPrefix(rawPath, "/DATA"), "/\\"))
	}
	abs, err := filepath.Abs(rawPath)
	if err != nil || fileName == "" || filepath.Base(fileName) != fileName || strings.ContainsAny(fileName, `\/:`) {
		return "", fmt.Errorf("invalid upload path or filename")
	}
	if relative == "" {
		relative = fileName
	}
	relative = filepath.FromSlash(strings.ReplaceAll(relative, "\\", "/"))
	if !filepath.IsLocal(relative) || filepath.Base(relative) != fileName {
		return "", fmt.Errorf("invalid relative upload path")
	}
	dir := filepath.Join(abs, filepath.Dir(relative))
	return dir, safefs.NoLinks(dir)
}

func uploadChunkDir(dir, name, id string, total, number int) (string, error) {
	if id == "" || len(id) > 256 {
		return "", fmt.Errorf("chunked uploads require a unique uploadId (or identifier), reused for every chunk")
	}
	if total < 2 || total > 100000 || number < 1 || number > total {
		return "", fmt.Errorf("invalid upload chunk")
	}
	key := sha256.Sum256([]byte(name + "\x00" + strconv.Itoa(total) + "\x00" + id))
	target := filepath.Join(dir, ".temp", fmt.Sprintf("%x", key))
	return target, safefs.NoLinks(target)
}

// Retries are idempotent, but cannot replace a complete part with different bytes.
func writeUploadChunk(dir string, number int, source io.Reader) error {
	f, err := os.CreateTemp(dir, ".chunk-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hash), source); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	target := filepath.Join(dir, strconv.Itoa(number))
	if err := safefs.NoReplace(f.Name(), target); err != nil {
		if _, statErr := os.Lstat(target); statErr != nil {
			return err
		}
		if err := safefs.NoLinks(target); err != nil {
			return err
		}
		old, err := os.Open(target)
		if err != nil {
			return err
		}
		defer old.Close()
		previous := sha256.New()
		if _, err := io.Copy(previous, old); err != nil {
			return err
		}
		if string(previous.Sum(nil)) != string(hash.Sum(nil)) {
			return fmt.Errorf("upload chunk already exists with different contents: %w", os.ErrExist)
		}
	}
	return nil
}

// Only published, numbered regular files count as complete upload chunks.
func uploadChunksReady(dir string, total int) (bool, error) {
	for number := 1; number <= total; number++ {
		info, err := os.Lstat(filepath.Join(dir, strconv.Itoa(number)))
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("invalid upload chunk %d", number)
		}
	}
	return true, nil
}
