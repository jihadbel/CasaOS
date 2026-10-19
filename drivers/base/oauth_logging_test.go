package base_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	"github.com/IceWhaleTech/CasaOS/drivers/base"
	"github.com/IceWhaleTech/CasaOS/drivers/dropbox"
	"github.com/IceWhaleTech/CasaOS/drivers/google_drive"
	"github.com/IceWhaleTech/CasaOS/drivers/onedrive"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
	"go.uber.org/zap/zapcore"
)

type tokenTransport struct {
	t      *testing.T
	grants map[string]int
}

func (transport tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	transport.t.Helper()
	if !strings.HasSuffix(req.URL.Path, "/token") {
		transport.t.Fatal("unexpected request during token test", req.URL.Path)
	}
	if err := req.ParseForm(); err != nil {
		transport.t.Fatal(err)
	}
	transport.grants[req.Form.Get("grant_type")]++
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"access_token":"oauth-test-access","refresh_token":"oauth-test-refresh","token_type":"Bearer","expires_in":3600}`)),
		Request:    req,
	}, nil
}

func TestOAuthTokensNeverAppearInApplicationOrDebugLogs(t *testing.T) {
	previousClient := base.RestyClient
	previousOutput, previousLevel := log.StandardLogger().Out, log.GetLevel()
	t.Cleanup(func() {
		base.RestyClient = previousClient
		log.SetOutput(previousOutput)
		log.SetLevel(previousLevel)
		logger.LogInitConsoleOnly()
	})
	google, dp, one := google_drive.GetConfig(), dropbox.GetConfig(), onedrive.GetConfig()
	for _, provider := range []struct {
		name string
		init func(context.Context) error
	}{{"GoogleDrive", google.Init}, {"Dropbox", dp.Init}, {"OneDrive", one.Init}} {
		t.Run(provider.name, func(t *testing.T) {
			var applicationLog, debugLog bytes.Buffer
			logger.LogInitWithWriterSyncers(zapcore.AddSync(&applicationLog))
			log.SetOutput(&debugLog)
			log.SetLevel(log.DebugLevel)
			grants := map[string]int{}
			base.RestyClient = resty.New().SetTransport(tokenTransport{t, grants})
			// Exercise the initial exchange and a subsequent refresh of the same account.
			for i := 0; i < 2; i++ {
				if err := provider.init(context.Background()); err != nil {
					t.Fatal("synthetic token exchange failed", err)
				}
			}
			if grants["authorization_code"] == 0 || grants["refresh_token"] == 0 {
				t.Fatal("both OAuth flows must execute", grants)
			}
			for _, output := range []string{applicationLog.String(), debugLog.String()} {
				if strings.Contains(output, "oauth-test-access") || strings.Contains(output, "oauth-test-refresh") {
					t.Fatal("token response leaked into a log")
				}
			}
		})
	}
}
