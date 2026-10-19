package v1

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	"github.com/IceWhaleTech/CasaOS/drivers/base"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/httper"
	"github.com/IceWhaleTech/CasaOS/service"
	"github.com/go-resty/resty/v2"
	"github.com/labstack/echo/v4"
)

type recoveryStorage struct {
	service.StorageService
	kind                          string
	existing                      bool
	createErr, mountErr           error
	created, mounted, reconnected int
}

func (s *recoveryStorage) GetConfig() (httper.RemotesResult, error) {
	if s.existing {
		return httper.RemotesResult{Remotes: []string{"existing"}}, nil
	}
	return httper.RemotesResult{}, nil
}
func (s *recoveryStorage) GetConfigByName(string) (map[string]string, error) {
	return map[string]string{"type": s.kind, "username": "review@example.test"}, nil
}
func (s *recoveryStorage) CreateConfig(map[string]string, string, string) error {
	s.created++
	return s.createErr
}
func (s *recoveryStorage) MountStorage(string, string) error {
	s.mounted++
	return s.mountErr
}
func (s *recoveryStorage) CheckAndMountByName(string) error {
	s.reconnected++
	return s.mountErr
}

type recoveryNotifications struct {
	service.NotifyServer
	statuses []string
}

func (n *recoveryNotifications) SendNotify(event string, message map[string]interface{}) {
	if event == "casaos:file:recover" {
		n.statuses = append(n.statuses, message["status"].(string))
	}
}

type recoveryRepository struct {
	service.Repository
	storage *recoveryStorage
	notify  *recoveryNotifications
}

func (r recoveryRepository) Storage() service.StorageService { return r.storage }
func (r recoveryRepository) Notify() service.NotifyServer    { return r.notify }

type recoveryProviderTransport struct{}

func (recoveryProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body string
	switch {
	case strings.HasSuffix(r.URL.Path, "/token"):
		body = `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","token_type":"Bearer"}`
	case r.URL.Host == "content.googleapis.com":
		body = `{"user":{"emailAddress":"review@example.test"}}`
	case r.URL.Host == "api.dropboxapi.com":
		body = `{"email":"review@example.test"}`
	case r.URL.Host == "graph.microsoft.com":
		body = `{"createdBy":{"user":{"email":"review@example.test"}},"parentReference":{"driveId":"review-drive","driveType":"personal"}}`
	default:
		return nil, errors.New("unexpected provider request in recovery test")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func TestCloudRecoveryReportsSaveAndMountFailuresForEveryProvider(t *testing.T) {
	previousService, previousClient := service.MyService, base.RestyClient
	t.Cleanup(func() { service.MyService = previousService; base.RestyClient = previousClient })
	logger.LogInitConsoleOnly()
	base.RestyClient = resty.New().SetTransport(recoveryProviderTransport{})
	backendErr := errors.New("synthetic backend failure")
	for _, provider := range []struct{ name, kind string }{{"GoogleDrive", "drive"}, {"Dropbox", "dropbox"}, {"Onedrive", "onedrive"}} {
		for _, check := range []struct {
			name                          string
			existing                      bool
			createErr, mountErr           error
			status                        int
			notification                  string
			created, mounted, reconnected int
		}{
			{"save failure", false, backendErr, nil, 502, "fail", 1, 0, 0},
			{"mount failure", false, nil, backendErr, 502, "fail", 1, 1, 0},
			{"success", false, nil, nil, 200, "success", 1, 1, 0},
			{"reconnect failure", true, nil, backendErr, 502, "fail", 0, 0, 1},
			{"already connected", true, nil, nil, 200, "warn", 0, 0, 1},
		} {
			t.Run(provider.name+"/"+check.name, func(t *testing.T) {
				storage := &recoveryStorage{kind: provider.kind, existing: check.existing, createErr: check.createErr, mountErr: check.mountErr}
				notifications := &recoveryNotifications{}
				service.MyService = recoveryRepository{storage: storage, notify: notifications}
				w := httptest.NewRecorder()
				ctx := echo.New().NewContext(httptest.NewRequest("GET", "/v1/recover/"+provider.name+"?code=synthetic-code", nil), w)
				ctx.SetParamNames("type")
				ctx.SetParamValues(provider.name)
				if err := GetRecoverStorage(ctx); err != nil {
					t.Fatal(err)
				}
				if w.Code != check.status || len(notifications.statuses) != 1 || notifications.statuses[0] != check.notification || storage.created != check.created || storage.mounted != check.mounted || storage.reconnected != check.reconnected {
					t.Fatal("incorrect recovery outcome", w.Code, notifications.statuses, storage.created, storage.mounted, storage.reconnected)
				}
			})
		}
	}
}
