package v1

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestDownloadTraversalStopsAtBudgetAndOnCancellation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	visit := func(string, os.FileInfo) error { count++; return nil }
	if err := walkDownloadTree(context.Background(), root, 2, visit); err == nil || count != 2 {
		t.Fatal(count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	count = 0
	if err := walkDownloadTree(ctx, root, 10, visit); err != context.Canceled || count != 0 {
		t.Fatal(count, err)
	}
	w := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(httptest.NewRequest("GET", "/", nil), w)
	filesystemRoot := filepath.VolumeName(root) + string(os.PathSeparator)
	if err := downloadDirectory(c, filesystemRoot); err == nil || w.Body.Len() > 0 {
		t.Fatal("root download accepted", err)
	}
}
