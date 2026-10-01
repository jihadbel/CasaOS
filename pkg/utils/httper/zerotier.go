package httper

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func getZeroTierPaths() (string, string) {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if len(programData) == 0 {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "ZeroTier", "One", "zerotier-one.port"),
			filepath.Join(programData, "ZeroTier", "One", "authtoken.secret")
	}
	return "/var/lib/zerotier-one/zerotier-one.port", "/var/lib/zerotier-one/authtoken.secret"
}

func ZTGet(url string) ([]byte, error) {
	portPath, authPath := getZeroTierPaths()
	port, err := ioutil.ReadFile(portPath)
	if err != nil {
		return nil, err
	}

	// Build the target URL
	targetURL := fmt.Sprintf("http://localhost:%s%s", strings.TrimSpace(string(port)), url)

	// Create a new request
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, err
	}

	// Add the X-ZT1-AUTH header
	authToken, err := ioutil.ReadFile(authPath)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-ZT1-AUTH", strings.TrimSpace(string(authToken)))

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return respBody, nil
}
func ZTPost(url string, body string) ([]byte, error) {
	portPath, authPath := getZeroTierPaths()
	port, err := ioutil.ReadFile(portPath)
	if err != nil {
		return nil, err
	}
	// Build the target URL
	targetURL := fmt.Sprintf("http://localhost:%s%s", strings.TrimSpace(string(port)), url)

	// Create a new request
	req, err := http.NewRequest("POST", targetURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Add the X-ZT1-AUTH header
	authToken, err := ioutil.ReadFile(authPath)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-ZT1-AUTH", strings.TrimSpace(string(authToken)))

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return respBody, nil
}
