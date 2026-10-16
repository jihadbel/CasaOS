package httper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestMountListHandlesEmptyAndUnterminatedFilesystemNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/mount/listmounts" {
			t.Error(r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"mountPoints":[{"Fs":""},{"Fs":"remote:"},{"Fs":"folder/path"},{"Fs":"دليل:"}]}`))
	}))
	defer server.Close()
	result, err := getMountList(resty.New().SetBaseURL(server.URL))
	if err != nil || len(result.MountPoints) != 4 {
		t.Fatal(result, err)
	}
	for index, want := range []string{"", "remote", "folder/path", "دليل"} {
		if result.MountPoints[index].Fs != want {
			t.Fatal(result.MountPoints[index].Fs, want)
		}
	}
}

func TestMountListReturnsInvalidJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"mountPoints":`)) }))
	defer server.Close()
	if _, err := getMountList(resty.New().SetBaseURL(server.URL)); err == nil {
		t.Fatal("malformed mount response accepted")
	}
}
