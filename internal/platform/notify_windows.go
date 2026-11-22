//go:build windows

package platform

import (
	"context"
	"fmt"
	"html"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

const aumid = "io.github.patrickryan01.halyard"

// registerAppID tells Windows who the toasts come from, so they carry the app's name and are not dropped as coming from an unknown app.
func registerAppID() {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+aumid, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue("DisplayName", "Halyard")
	if exe, err := os.Executable(); err == nil {
		_ = k.SetStringValue("IconUri", exe)
	}
	_ = k.SetDWordValue("ShowInSettings", 1)
}

// Notify raises a Windows toast through the WinRT toast API, called from PowerShell so no COM setup is needed in this process.
func Notify(title, body string) error {
	registerAppID()
	xml := fmt.Sprintf(`<toast><visual><binding template="ToastGeneric"><text>%s</text><text>%s</text></binding></visual></toast>`, html.EscapeString(title), html.EscapeString(body))
	script := `$ErrorActionPreference='Stop'
[Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime] | Out-Null
$x = New-Object Windows.Data.Xml.Dom.XmlDocument
$x.LoadXml('` + escapeSingle(xml) + `')
$t = [Windows.UI.Notifications.ToastNotification]::new($x)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('` + aumid + `').Show($t)`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("toast: %w: %s", err, out)
	}
	return nil
}

func escapeSingle(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\'')
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}
