//go:build windows

package updates

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Launch writes the helper script to dir and starts it, detached and hidden,
// so it keeps running after the app exits.
func Launch(script, dir string) error {
	path := filepath.Join(dir, "apply-update.ps1")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", path)
	const detached, newGroup = 0x00000008, 0x00000200
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: detached | newGroup}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
