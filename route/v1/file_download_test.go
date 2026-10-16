package v1

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"io"
	"mime"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestDownloadContainsActiveFilesAndPreservesMediaRangeRequests(t *testing.T) {
	root := t.TempDir()
	for _, entry := range []struct{ name, disposition string }{
		{"attack.html", "attachment"}, {"attack.svg", "attachment"}, {"attack.xhtml", "attachment"}, {"unknown.bin", "attachment"},
		{"photo.jpg", "inline"}, {"movie.mp4", "inline"}, {"song.mp3", "inline"}, {"document.pdf", "inline"}, {"plain.txt", "inline"},
	} {
		t.Run(entry.name, func(t *testing.T) {
			path := filepath.Join(root, entry.name)
			if err := os.WriteFile(path, []byte("<script>attack()</script>"), 0600); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/v1/file?path="+url.QueryEscape(path), nil)
			req.Header.Set("Range", "bytes=0-3")
			w := httptest.NewRecorder()
			if err := GetDownloadSingleFile(echo.New().NewContext(req, w)); err != nil {
				t.Fatal(err)
			}
			disposition, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
			if err != nil || disposition != entry.disposition || params["filename"] != entry.name {
				t.Fatal(w.Header(), err)
			}
			if w.Code != 206 || w.Body.String() != "<scr" || w.Header().Get("Content-Range") == "" {
				t.Fatal("range request broken", w.Code, w.Body.String(), w.Header())
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") == "" {
				t.Fatal(w.Header())
			}
			if entry.disposition == "attachment" && !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
				t.Fatal("active content not sandboxed", w.Header())
			}
			if req.Header.Get("Content-Disposition") != "" {
				t.Fatal("response headers written to request")
			}
			if entry.disposition == "attachment" {
				imageResponse := httptest.NewRecorder()
				imageReq := httptest.NewRequest("GET", "/v1/image?path="+url.QueryEscape(path), nil)
				if err := GetFileImage(echo.New().NewContext(imageReq, imageResponse)); err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(imageResponse.Header().Get("Content-Disposition"), "attachment") {
					t.Fatal("image endpoint bypasses download protection", imageResponse.Header())
				}
			}
		})
	}
}

func TestDownloadFolderReturnsCompleteZipForShareLink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Shared")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "text.txt"), []byte("folder contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".temp"), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, ".temp", "partial.txt"), []byte("partial upload"), 0600)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/file?path="+url.QueryEscape(root), nil)
	if err := GetDownloadSingleFile(echo.New().NewContext(req, w)); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(w.Code, w.Header())
	}
	_, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil || params["filename"] != "Shared.zip" {
		t.Fatal(w.Header(), err)
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range z.File {
		if strings.Contains(f.Name, ".temp") {
			t.Fatal("partial upload leaked", f.Name)
		}
		if f.Name == "Shared/nested/text.txt" {
			reader, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(data) != "folder contents" {
				t.Fatal(string(data), err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("folder contents missing")
	}
}

func TestDownloadThumbnailContainsOnlyEncodedImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.png")
	var original bytes.Buffer
	if err := png.Encode(&original, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/image?type=thumbnail&path="+url.QueryEscape(path), nil)
	if err := GetFileImage(echo.New().NewContext(req, w)); err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(w.Body.Bytes())
	img, err := png.Decode(reader)
	if err != nil || w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal(w.Code, w.Header(), err)
	}
	if img.Bounds().Dx() != 100 || reader.Len() != 0 {
		t.Fatal("thumbnail includes trailing original file data", img.Bounds(), reader.Len())
	}
}

func TestBulkSingleDownloadUsesSafeBasenameAndHeaders(t *testing.T) {
	name := "صورة العائلة.html"
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("<script>unsafe()</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/file/download?files="+url.QueryEscape(path), nil)
	if err := GetDownloadFile(e.NewContext(req, w)); err != nil {
		t.Fatal(err)
	}
	disposition, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil || disposition != "attachment" || params["filename"] != name {
		t.Fatal(w.Header(), err)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("bulk download bypassed safe headers", w.Header())
	}
}

func TestSingleDownloadRejectsInternalStorage(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".temp", ".TEMP", ".CasaOS-RecycleBin", ".casaos-upload-test"} {
		path := filepath.Join(root, dir, "secret.txt")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, thumbnail := range []bool{false, true} {
			e := echo.New()
			w := httptest.NewRecorder()
			ctx := e.NewContext(httptest.NewRequest("GET", "/v1/file?type=thumbnail&path="+url.QueryEscape(path), nil), w)
			handler := GetDownloadSingleFile
			if thumbnail {
				handler = GetFileImage
			}
			if err := handler(ctx); err != nil {
				e.HTTPErrorHandler(err, ctx)
			}
			if w.Code != 400 || strings.Contains(w.Body.String(), "private") {
				t.Fatal(dir, w.Code, w.Body.String())
			}
		}
	}
}
