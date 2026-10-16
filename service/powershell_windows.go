package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Scripts are fixed source; values only enter through environment variables.
func runSMBScript(script string, values map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	cmd := exec.CommandContext(ctx, filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference = 'Stop'; "+script)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "CASAOS_SMB_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range values {
		cmd.Env = append(cmd.Env, "CASAOS_SMB_"+key+"="+value)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Windows SMB operation failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
