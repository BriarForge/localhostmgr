// Package launchd installs and uninstalls the LaunchAgent that keeps
// `localhostmgr serve` alive across reboots and logouts.
package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const Label = "com.briarforge.localhostmgr"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

// Install writes the LaunchAgent plist and loads it. If a previous plist is
// already loaded, it's unloaded first so PATH / binary updates take effect.
func Install(binaryPath string) error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.briarforge.localhostmgr</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + binaryPath + `</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/usr/local/bin:/usr/local/Cellar/node@22/22.22.3/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    <key>HOME</key>
    <string>` + homeDir() + `</string>
  </dict>
  <key>StandardOutPath</key>
  <string>` + homePath("Library/Logs/localhostmgr.out.log") + `</string>
  <key>StandardErrorPath</key>
  <string>` + homePath("Library/Logs/localhostmgr.err.log") + `</string>
</dict>
</plist>
`
	// Best-effort unload of any existing instance before rewriting the plist.
	_ = exec.Command("launchctl", "unload", "-w", p).Run()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return err
	}
	return exec.Command("launchctl", "load", "-w", p).Run()
}

// Uninstall stops, unloads, and removes the LaunchAgent.
func Uninstall() error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", "-w", p).Run()
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Installed reports whether the plist is currently on disk.
func Installed() (bool, string, error) {
	p, err := plistPath()
	if err != nil {
		return false, "", err
	}
	_, err = os.Stat(p)
	if err == nil {
		return true, p, nil
	}
	if os.IsNotExist(err) {
		return false, p, nil
	}
	return false, p, err
}

// BootoutAndLoad is a no-op kept for symmetry; not used by Install above.
var _ = fmt.Sprintf
