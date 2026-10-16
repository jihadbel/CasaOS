package file

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
)

func copyFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	for _, dir := range []string{source, target} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return source, target
}

func putCopyFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func requireCopyFile(t *testing.T, path, want string) {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil || string(value) != want {
		t.Fatalf("%s: got %q, %v; want %q", path, value, err, want)
	}
}

func TestMovePublishesCompleteReplacementAndRetainsSkippedSource(t *testing.T) {
	for _, directory := range []bool{false, true} {
		for _, style := range []string{"overwrite", "skip"} {
			t.Run(style+map[bool]string{false: "-file", true: "-directory"}[directory], func(t *testing.T) {
				sourceRoot, targetRoot := copyFixture(t)
				source := filepath.Join(sourceRoot, "item")
				target := filepath.Join(targetRoot, "item")
				srcFile, dstFile := source, target
				if directory {
					srcFile = filepath.Join(source, "nested", "data.txt")
					dstFile = filepath.Join(target, "nested", "data.txt")
				}
				putCopyFile(t, srcFile, "new bytes")
				putCopyFile(t, dstFile, "old bytes")
				if err := MovePathContext(context.Background(), source, targetRoot, style); err != nil {
					t.Fatal(err)
				}
				if style == "skip" {
					requireCopyFile(t, srcFile, "new bytes")
					requireCopyFile(t, dstFile, "old bytes")
					return
				}
				requireCopyFile(t, dstFile, "new bytes")
				if _, err := os.Stat(source); !os.IsNotExist(err) {
					t.Fatalf("source not removed after successful move: %v", err)
				}
			})
		}
	}
}

func TestCancelledMoveRetainsBothExistingTrees(t *testing.T) {
	sourceRoot, targetRoot := copyFixture(t)
	source, target := filepath.Join(sourceRoot, "item"), filepath.Join(targetRoot, "item")
	putCopyFile(t, filepath.Join(source, "data.txt"), "source")
	putCopyFile(t, filepath.Join(target, "data.txt"), "destination")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MovePathContext(ctx, source, targetRoot, "overwrite"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	requireCopyFile(t, filepath.Join(source, "data.txt"), "source")
	requireCopyFile(t, filepath.Join(target, "data.txt"), "destination")
	entries, err := os.ReadDir(targetRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging files leaked: %v %v", entries, err)
	}
}

func TestMoveRetainsDestinationWhenNestedCopyFails(t *testing.T) {
	sourceRoot, targetRoot := copyFixture(t)
	source, target := filepath.Join(sourceRoot, "item"), filepath.Join(targetRoot, "item")
	putCopyFile(t, filepath.Join(source, "a.txt"), "source")
	putCopyFile(t, filepath.Join(target, "a.txt"), "destination")
	if err := os.Symlink(filepath.Join(sourceRoot, "missing"), filepath.Join(source, "z-link")); err != nil {
		t.Skipf("creating test symlink requires platform support: %v", err)
	}
	if err := MovePathContext(context.Background(), source, targetRoot, "overwrite"); err == nil {
		t.Fatal("nested error was suppressed")
	}
	requireCopyFile(t, filepath.Join(source, "a.txt"), "source")
	requireCopyFile(t, filepath.Join(target, "a.txt"), "destination")
}

func TestCopyRejectsSelfDescendantsAndHardLinkAliases(t *testing.T) {
	sourceRoot, _ := copyFixture(t)
	putCopyFile(t, filepath.Join(sourceRoot, "file.txt"), "source")
	for _, parent := range []string{filepath.Dir(sourceRoot), sourceRoot, filepath.Join(sourceRoot, "nested")} {
		if err := CopyDir(sourceRoot, parent, "overwrite"); err == nil {
			t.Fatalf("accepted overlapping destination %s", parent)
		}
	}
	file := filepath.Join(sourceRoot, "file.txt")
	alias := filepath.Join(sourceRoot, "alias.txt")
	if err := os.Link(file, alias); err != nil {
		t.Skipf("hard links unsupported: %v", err)
	}
	if err := CopySingleFile(file, alias, "overwrite"); err == nil {
		t.Fatal("accepted hard-link alias")
	}
	requireCopyFile(t, file, "source")
}

type cancellingCopySource struct{ cancel context.CancelFunc }

func (r cancellingCopySource) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, []byte("partial replacement")), nil
}

func TestInterruptedStreamNeverReplacesDestination(t *testing.T) {
	target := filepath.Join(t.TempDir(), "existing.txt")
	putCopyFile(t, target, "original")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := safefs.WriteAtomicReader(target, copyReader{ctx, cancellingCopySource{cancel}}, 0600)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected copy failure, got %v", err)
	}
	requireCopyFile(t, target, "original")
}
