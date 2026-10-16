package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

func apiRequest(t *testing.T, handler http.HandlerFunc, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func makeTestZIP(t *testing.T, path, name, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, content); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveRoundTripExcludesOwnOutput(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "album")
	writeTestFile(t, filepath.Join(folder, "a.txt"), "original")
	output := filepath.Join(folder, "album.zip")
	response := apiRequest(t, handleZip, "POST", "/api/zip", ZipRequest{Files: []string{folder}, Output: output})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	z, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if strings.Contains(f.Name, ".zip") || strings.Contains(f.Name, ".casaos-archive-") {
			t.Fatal("archive included its own output", f.Name)
		}
	}
	z.Close()
	dest := filepath.Join(root, "extracted")
	response = apiRequest(t, handleUnzip, "POST", "/api/unzip", UnzipRequest{Path: output, Dest: dest})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(dest, "album", "a.txt"))
	if err != nil || string(data) != "original" {
		t.Fatal(string(data), err)
	}
}

func TestArchiveRejectsWindowsAlternateDataStreamOutput(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NTFS-specific")
	}
	root := t.TempDir()
	input := filepath.Join(root, "small.txt")
	writeTestFile(t, input, "data")
	w := apiRequest(t, handleZip, "POST", "/api/zip", ZipRequest{Files: []string{input}, Output: filepath.Join(root, "C:.zip")})
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "C")); !os.IsNotExist(err) {
		t.Fatal("alternate stream base file was created", err)
	}
}

func TestArchiveNeverTruncatesSourceOrExistingOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "original.zip")
	makeTestZIP(t, source, "a.txt", "keep")
	before, _ := os.ReadFile(source)
	response := apiRequest(t, handleZip, "POST", "/api/zip", ZipRequest{Files: []string{source}})
	after, _ := os.ReadFile(source)
	if response.Code != 409 || !bytes.Equal(before, after) {
		t.Fatal(response.Code, "source changed")
	}
	output := filepath.Join(root, "output.zip")
	writeTestFile(t, output, "existing")
	response = apiRequest(t, handleZip, "POST", "/api/zip", ZipRequest{Files: []string{source}, Output: output})
	after, _ = os.ReadFile(output)
	if response.Code != 409 || string(after) != "existing" {
		t.Fatal(response.Code, "output changed")
	}
}

func TestUnzipRejectsUnsafePathsBeforeWriting(t *testing.T) {
	for _, name := range []string{"../extract_escape/proof.txt", `..\extract_escape\proof.txt`, "/absolute.txt", "C:/absolute.txt", "file.txt:stream", "nested/../../escape.txt"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "in.zip")
			dest := filepath.Join(root, "extract")
			makeTestZIP(t, input, name, "must not escape")
			response := apiRequest(t, handleUnzip, "POST", "/api/unzip", UnzipRequest{Path: input, Dest: dest})
			if response.Code != 400 {
				t.Fatal(response.Code, response.Body.String())
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("destination published despite invalid archive", err)
			}
			if _, err := os.Stat(filepath.Join(root, "extract_escape", "proof.txt")); !os.IsNotExist(err) {
				t.Fatal("escaped destination")
			}
		})
	}
}

func TestUnzipCRCFailureDoesNotPublishPartialFiles(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "in.zip")
	dest := filepath.Join(root, "extract")
	makeTestZIP(t, input, "a.txt", "unique-payload")
	data, _ := os.ReadFile(input)
	offset := bytes.Index(data, []byte("unique-payload"))
	if offset < 0 {
		t.Fatal("payload missing")
	}
	data[offset] ^= 1
	os.WriteFile(input, data, 0600)
	response := apiRequest(t, handleUnzip, "POST", "/api/unzip", UnzipRequest{Path: input, Dest: dest})
	if response.Code == 200 {
		t.Fatal("false success", response.Body.String())
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("partial extraction published")
	}
}

func TestUnzipPreservesExistingDestination(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "in.zip")
	dest := filepath.Join(root, "extract")
	writeTestFile(t, filepath.Join(dest, "a.txt"), "keep")
	makeTestZIP(t, input, "a.txt", "replace")
	response := apiRequest(t, handleUnzip, "POST", "/api/unzip", UnzipRequest{Path: input, Dest: dest})
	data, _ := os.ReadFile(filepath.Join(dest, "a.txt"))
	if response.Code != 409 || string(data) != "keep" {
		t.Fatal(response.Code, string(data))
	}
}

func setupTrashTest(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old := RecycleBinDir
	RecycleBinDir = filepath.Join(root, "trash")
	t.Cleanup(func() { RecycleBinDir = old })
	return root
}

func TestTimelineExcludesTrashHiddenAndStagingDirectories(t *testing.T) {
	root := setupTrashTest(t)
	old := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = old })
	t.Setenv("CASAOS_DATA_ROOT", root)
	settings := defaultStorageSettings()
	settings.PhotosPath = root
	if w := apiRequest(t, handleStorageSettings, "PUT", "/api/settings", settings); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	photo := filepath.Join(root, "deleted.jpg")
	writeTestFile(t, photo, "photo")
	item := moveTestItem(t, photo)
	writeTestFile(t, filepath.Join(root, "visible.jpg"), "photo")
	for _, dir := range []string{".CasaOS-RecycleBin", ".casaos-extract-test", ".temp", ".hidden", "$RECYCLE.BIN"} {
		writeTestFile(t, filepath.Join(root, dir, "hidden.jpg"), "photo")
	}
	check := func(query string, want int) {
		t.Helper()
		w := apiRequest(t, handleMediaTimeline, "GET", "/api/media/timeline"+query, nil)
		var result struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Total != want {
			t.Fatal(w.Code, w.Body.String(), err)
		}
	}
	check("", 1)
	check("?folder="+url.QueryEscape(filepath.Dir(item.TrashPath)), 0)
	check("?folder="+url.QueryEscape(filepath.Join(root, ".hidden")), 0)
}

func moveTestItem(t *testing.T, path string) TrashItem {
	t.Helper()
	response := apiRequest(t, handleTrash, "POST", "/api/trash/move", map[string]string{"path": path})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var data struct {
		Item TrashItem `json:"item"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	return data.Item
}

func TestTrashRestoresRequestedItemOnly(t *testing.T) {
	root := setupTrashTest(t)
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	writeTestFile(t, a, "A")
	writeTestFile(t, b, "B")
	first := moveTestItem(t, a)
	second := moveTestItem(t, b)
	response := apiRequest(t, handleTrash, "POST", "/api/trash/restore", map[string]string{"id": first.ID})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	data, err := os.ReadFile(a)
	if err != nil || string(data) != "A" {
		t.Fatal("requested item not restored", err)
	}
	if _, err := os.Stat(second.TrashPath); err != nil {
		t.Fatal("unrelated item moved", err)
	}
	items, err := loadTrashManifest()
	if err != nil || len(items) != 1 || items[0].ID != second.ID {
		t.Fatal(items, err)
	}
}

func TestTrashConcurrentMovesPreserveEveryRecord(t *testing.T) {
	root := setupTrashTest(t)
	const count = 16
	for i := 0; i < count; i++ {
		writeTestFile(t, filepath.Join(root, fmt.Sprint(i)), "keep")
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"path": filepath.Join(root, fmt.Sprint(i))})
			w := httptest.NewRecorder()
			handleTrash(w, httptest.NewRequest("POST", "/api/trash/move", bytes.NewReader(body)))
			results <- w
		}(i)
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.Code != 200 {
			t.Fatal(result.Code, result.Body.String())
		}
	}
	items, err := loadTrashManifest()
	if err != nil || len(items) != count {
		t.Fatal(len(items), err)
	}
}

func TestTrashCorruptIndexPreservesSourceAndIndex(t *testing.T) {
	root := setupTrashTest(t)
	source := filepath.Join(root, "keep.txt")
	writeTestFile(t, source, "keep")
	writeTestFile(t, trashManifestPath(), "{damaged")
	response := apiRequest(t, handleTrash, "POST", "/api/trash/move", map[string]string{"path": source})
	data, _ := os.ReadFile(source)
	index, _ := os.ReadFile(trashManifestPath())
	if response.Code != 500 || string(data) != "keep" || string(index) != "{damaged" {
		t.Fatal(response.Code, string(data), string(index))
	}
}

func TestTrashRestoreNeverOverwritesExistingFile(t *testing.T) {
	root := setupTrashTest(t)
	path := filepath.Join(root, "keep.txt")
	writeTestFile(t, path, "old")
	item := moveTestItem(t, path)
	writeTestFile(t, path, "new")
	response := apiRequest(t, handleTrash, "POST", "/api/trash/restore", map[string]string{"id": item.ID})
	data, _ := os.ReadFile(path)
	old, _ := os.ReadFile(item.TrashPath)
	if response.Code != 409 || string(data) != "new" || string(old) != "old" {
		t.Fatal(response.Code, string(data), string(old))
	}
}

func TestTrashRejectsIndexPointingOutsideBin(t *testing.T) {
	root := setupTrashTest(t)
	outside := filepath.Join(root, "outside")
	writeTestFile(t, outside, "keep")
	data, _ := json.Marshal([]TrashItem{{ID: "fake", OriginalPath: outside, TrashPath: outside}})
	writeTestFile(t, trashManifestPath(), string(data))
	response := apiRequest(t, handleTrash, "POST", "/api/trash/delete", map[string]string{"id": "fake"})
	if response.Code != 500 {
		t.Fatal(response.Code, response.Body.String())
	}
	if data, _ := os.ReadFile(outside); string(data) != "keep" {
		t.Fatal("deleted outside bin")
	}
}

func TestTrashChoosesSameVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows volume paths")
	}
	setupTrashTest(t)
	source := `Z:\photos\a.jpg`
	if !strings.EqualFold(filepath.VolumeName(source), filepath.VolumeName(trashDirectory(source))) {
		t.Fatal("cross-volume bin")
	}
}

func TestSettingsAndTimelineUseSharedPathsAndMediaTypes(t *testing.T) {
	root := t.TempDir()
	old := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "config")
	t.Cleanup(func() { config.AppInfo.UserDataPath = old })
	s := StorageSettings{true, filepath.Join(root, "apps"), filepath.Join(root, "archives"), filepath.Join(root, "photos"), filepath.Join(root, "videos"), filepath.Join(root, "docs"), filepath.Join(root, "music"), filepath.Join(root, "mobile"), filepath.Join(root, "data")}
	response := apiRequest(t, handleStorageSettings, "PUT", "/api/settings", s)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	settings, configured, err := readStorageSettings()
	if err != nil || !configured || settings.PhotosPath != s.PhotosPath {
		t.Fatal(settings, err)
	}
	writeTestFile(t, filepath.Join(s.PhotosPath, "a.avif"), "avif")
	writeTestFile(t, filepath.Join(s.PhotosPath, "a.tiff"), "tiff")
	writeTestFile(t, filepath.Join(s.VideosPath, "a.m4v"), "m4v")
	writeTestFile(t, filepath.Join(s.VideosPath, "code.ts"), "typescript")
	response = apiRequest(t, handleMediaTimeline, "GET", "/api/media/timeline", nil)
	var data struct {
		Total int `json:"total"`
	}
	json.Unmarshal(response.Body.Bytes(), &data)
	if response.Code != 200 || data.Total != 3 {
		t.Fatal(response.Code, response.Body.String())
	}
}
