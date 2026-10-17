package v1

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

type zeroTierTransport func(*http.Request) (*http.Response, error)

func (f zeroTierTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestZeroTierProxyStopsOnMissingOrInvalidCredentials(t *testing.T) {
	for _, entry := range []struct {
		name, port, token       string
		createPort, createToken bool
	}{
		{"missing-port", "", "", false, false},
		{"invalid-port", "0", "secret", true, true},
		{"injected-port", "9993/other", "secret", true, true},
		{"missing-token", "9993", "", true, false},
		{"empty-token", "9993", " ", true, true},
	} {
		t.Run(entry.name, func(t *testing.T) {
			root := t.TempDir()
			port, auth := filepath.Join(root, "port"), filepath.Join(root, "token")
			if entry.createPort {
				if err := os.WriteFile(port, []byte(entry.port), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if entry.createToken {
				if err := os.WriteFile(auth, []byte(entry.token), 0600); err != nil {
					t.Fatal(err)
				}
			}
			client := &http.Client{Transport: zeroTierTransport(func(*http.Request) (*http.Response, error) {
				t.Error("request sent after credential failure")
				return nil, errors.New("unexpected call")
			})}
			e := echo.New()
			w := httptest.NewRecorder()
			ctx := e.NewContext(httptest.NewRequest("GET", "/v1/zt/status", nil), w)
			if err := zerotierProxy(ctx, port, auth, client); err != nil {
				e.HTTPErrorHandler(err, ctx)
			}
			if w.Code != 503 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestZeroTierProxyPreservesRequestAndUpstreamFailures(t *testing.T) {
	root := t.TempDir()
	port, token := filepath.Join(root, "port"), filepath.Join(root, "token")
	if err := os.WriteFile(port, []byte("9993\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("real-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		e := echo.New()
		request := httptest.NewRequest("POST", "/v1/zt/network?name=a%20b", strings.NewReader("request body"))
		request.Header.Add("X-ZT1-AUTH", "attacker-value")
		request.Header.Set("Content-Type", "application/json")
		ctx, cancel := context.WithCancel(request.Context())
		defer cancel()
		request = request.WithContext(ctx)
		client := &http.Client{Transport: zeroTierTransport(func(r *http.Request) (*http.Response, error) {
			if r.Context() != ctx || r.URL.Host != "127.0.0.1:9993" || r.URL.RequestURI() != "/network?name=a%20b" || len(r.Header.Values("X-ZT1-AUTH")) != 1 || r.Header.Get("X-ZT1-AUTH") != "real-secret" {
				t.Fatal(r.URL, r.Header)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || string(body) != "request body" {
				t.Fatal(string(body), err)
			}
			if fail {
				return nil, errors.New("connection refused")
			}
			return &http.Response{StatusCode: 409, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":"already joined"}`))}, nil
		})}
		w := httptest.NewRecorder()
		c := e.NewContext(request, w)
		if err := zerotierProxy(c, port, token, client); err != nil {
			e.HTTPErrorHandler(err, c)
		}
		if fail && w.Code != 502 || !fail && (w.Code != 409 || w.Body.String() != `{"error":"already joined"}`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestProxyStripsHopHeadersAndConnectionNamedFields(t *testing.T) {
	source := http.Header{
		"Connection":         {"keep-alive, X-Transport-Secret"},
		"X-Transport-Secret": {"hidden"}, "Transfer-Encoding": {"chunked"},
		"Content-Length": {"123"}, "Keep-Alive": {"timeout=5"}, "Trailer": {"Digest"},
		"Content-Type": {"application/json"}, "Set-Cookie": {"a=1", "b=2"},
	}
	destination := http.Header{}
	copyHeaders(destination, source)
	for _, key := range []string{"Connection", "X-Transport-Secret", "Transfer-Encoding", "Content-Length", "Keep-Alive", "Trailer"} {
		if destination.Get(key) != "" {
			t.Fatal("transport header forwarded", key)
		}
	}
	if destination.Get("Content-Type") != "application/json" || len(destination.Values("Set-Cookie")) != 2 {
		t.Fatal(destination)
	}
	if source.Get("Connection") == "" {
		t.Fatal("source headers mutated")
	}
}
