package file

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
)

func TestSizeWalkReturnsMissingPathError(t *testing.T) {
	if _, err := DirSizeB(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing path must return an error")
	}
}

func TestWindowsUserPathsRejectDeviceNamespacesAndStreams(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path syntax")
	}
	for _, path := range []string{`C:\data\file.txt:secret`, `\\?\C:\ProgramData\CasaOS\conf\password.hash`, `\\.\C:\Windows\file`, `\??\C:\Windows\file`} {
		if _, err := ValidateUserPath(path); err == nil {
			t.Fatal("unsafe Windows path accepted", path)
		}
	}
}

func TestArchiveSelectionSharesBudgetAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	var paths []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if _, err := CollectArchiveEntries(context.Background(), paths, 2); err == nil {
		t.Fatal("multiple roots bypassed shared budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CollectArchiveEntries(ctx, paths, 10); err != context.Canceled {
		t.Fatal(err)
	}
	if err := CopyArchiveFile(ctx, io.Discard, bytes.NewReader([]byte("data")), 4); err != context.Canceled {
		t.Fatal(err)
	}
	if err := CopyArchiveFile(context.Background(), io.Discard, bytes.NewReader([]byte("grew")), 3); err == nil {
		t.Fatal("growing source accepted")
	}
	if err := CopyArchiveFile(context.Background(), io.Discard, bytes.NewReader([]byte("short")), 8); err == nil {
		t.Fatal("shrinking source accepted")
	}
}

func TestCopyAndMoveProtectInternalDataAndConfiguredRoots(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CASAOS_DATA_ROOT", filepath.Join(root, "data"))
	previous := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = previous })
	if err := os.MkdirAll(config.AppInfo.UserDataPath, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(config.AppInfo.UserDataPath, "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	public := filepath.Join(root, "public")
	if err := os.MkdirAll(public, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ValidateCopyTarget(secret, public); err == nil {
		t.Fatal("internal source accepted")
	}
	if _, _, err := ValidateCopyTarget(public, config.AppInfo.UserDataPath); err == nil {
		t.Fatal("internal target accepted")
	}
	if err := CopyDirContext(context.Background(), root, t.TempDir(), "overwrite"); err == nil {
		t.Fatal("nested internal source copied")
	}
	if err := MovePathContext(context.Background(), root, t.TempDir(), "overwrite"); err == nil {
		t.Fatal("ancestor of internal data moved")
	}
	settings := storagepaths.Default()
	settings.PhotosPath = filepath.Join(root, "separate-photos")
	encoded, _ := json.Marshal(settings)
	if err := os.WriteFile(storagepaths.File(), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDeletePath(settings.PhotosPath); err == nil {
		t.Fatal("configured storage root may be removed")
	}
	if data, err := os.ReadFile(secret); err != nil || string(data) != "private" {
		t.Fatal(string(data), err)
	}
}

func TestThumbnailRejectsDecompressionBombBeforeDecode(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := pngData.Bytes()
	// Keep a valid PNG header without allocating a giant source image.
	binary.BigEndian.PutUint32(data[16:20], 20000)
	binary.BigEndian.PutUint32(data[20:24], 20000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	path := filepath.Join(t.TempDir(), "bomb.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := GetThumbnailByWebPhoto(path, 100, 0); err == nil {
		t.Fatal("oversized image accepted")
	}
	if _, err := GetThumbnailByWebPhoto(path, 100000, 0); err == nil {
		t.Fatal("oversized output accepted")
	}
}
