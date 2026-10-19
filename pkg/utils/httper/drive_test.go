package httper

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	"github.com/go-resty/resty/v2"
	"go.uber.org/zap/zapcore"
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

func TestCreateConfigPropagatesHTTPFailuresWithoutLoggingResponseSecrets(t *testing.T) {
	t.Cleanup(logger.LogInitConsoleOnly)
	for _, status := range []int{200, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var logs bytes.Buffer
			logger.LogInitWithWriterSyncers(zapcore.AddSync(&logs))
			payload := `{"token":"rclone-test-token","client_secret":"rclone-test-secret"}`
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != "POST" || r.URL.Path != "/config/create" {
					t.Error("unexpected configuration request", r.Method, r.URL.Path)
				}
				w.WriteHeader(status)
				w.Write([]byte(payload))
			}))
			defer server.Close()
			err := createConfig(resty.New().SetBaseURL(server.URL), map[string]string{"token": "rclone-test-token"}, "review", "drive")
			if !called || (err != nil) != (status != 200) {
				t.Fatal("upstream outcome was not propagated", called, status, err)
			}
			if strings.Contains(logs.String(), payload) || strings.Contains(logs.String(), base64.StdEncoding.EncodeToString([]byte(payload))) || strings.Contains(logs.String(), "rclone-test-token") {
				t.Fatal("configuration response secrets were logged")
			}
		})
	}
}

type configTransportError struct{}

func (configTransportError) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic backend unavailable")
}

func TestCreateConfigReturnsTransportFailureWithoutDereferencingResponse(t *testing.T) {
	client := resty.New().SetBaseURL("http://rclone.test").SetTransport(configTransportError{})
	if err := createConfig(client, map[string]string{}, "review", "drive"); err == nil {
		t.Fatal("transport error was lost")
	}
}
