package v1

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func TestWindowsSambaStatusUsesNativeServiceWithoutLinuxTools(t *testing.T) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		t.Skip("Windows service manager unavailable:", err)
	}
	defer windows.CloseServiceHandle(manager)
	name, _ := windows.UTF16PtrFromString("LanmanServer")
	handle, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		t.Skip("Windows SMB server unavailable:", err)
	}
	server := &mgr.Service{Name: "LanmanServer", Handle: handle}
	defer server.Close()
	state, err := server.Query()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	w := httptest.NewRecorder()
	if err := GetSambaStatus(echo.New().NewContext(httptest.NewRequest("GET", "/v1/samba/shares/status", nil), w)); err != nil {
		t.Fatal(err)
	}
	want := 500
	if state.State == svc.Running {
		want = 200
	}
	if w.Code != want {
		t.Fatal("SMB status disagrees with Windows", state.State, w.Code, w.Body.String())
	}
	if want == 200 {
		var result struct {
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data["need_init"] != "false" {
			t.Fatal(w.Body.String(), err)
		}
	}
}
