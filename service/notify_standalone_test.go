package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/IceWhaleTech/CasaOS-Common/external"
	"github.com/IceWhaleTech/CasaOS/codegen/message_bus"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

type notifyProbeRepository struct {
	Repository
	calls *atomic.Int32
}

func (r notifyProbeRepository) MessageBus() *message_bus.ClientWithResponses {
	r.calls.Add(1)
	return r.Repository.MessageBus()
}

func TestStandaloneNotificationsSkipMissingBusAndResumeWhenDiscovered(t *testing.T) {
	previous, runtime := MyService, config.CommonInfo.RuntimePath
	t.Cleanup(func() { MyService = previous; config.CommonInfo.RuntimePath = runtime })
	config.CommonInfo.RuntimePath = t.TempDir()
	var calls, published atomic.Int32
	MyService = notifyProbeRepository{NewService(nil, config.CommonInfo.RuntimePath), &calls}
	for i := 0; i < 5; i++ {
		MyService.Notify().SendNotify("casaos:system:utilization", map[string]interface{}{"cpu": 0})
	}
	if calls.Load() != 0 {
		t.Fatal("standalone mode attempted bus publishing", calls.Load())
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { published.Add(1); w.WriteHeader(200) }))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(config.CommonInfo.RuntimePath, external.MessageBusAddressFilename), []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	MyService.Notify().SendNotify("casaos:system:utilization", map[string]interface{}{"cpu": 1})
	if calls.Load() != 1 || published.Load() != 1 {
		t.Fatal("integration did not resume", calls.Load(), published.Load())
	}
}
