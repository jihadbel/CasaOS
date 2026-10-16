package service

import "testing"

func TestHealthQueriesWindowsServicesWithoutLinuxTools(t *testing.T) {
	status, err := NewHealthService().Services()
	if err != nil {
		t.Fatal(err)
	}
	if status[true] == nil || status[false] == nil {
		t.Fatal("missing service status lists")
	}
}
