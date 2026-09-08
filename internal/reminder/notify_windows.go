package reminder

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type windowsNotifier struct{ mu sync.Mutex }

func NewNotifier() Notifier { return &windowsNotifier{} }

// 静态脚本只接收 Base64 编码的数据，不拼接用户输入。
// 设置仅写入当前用户的应用标识，不需要管理员权限。
const toastScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
$xmlText = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:DESKTOP_REMINDER_XML))
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($xmlText)
$appId = 'DesktopReminder.Silent'
$key = 'HKCU:\Software\Classes\AppUserModelId\' + $appId
$null = New-Item -Path $key -Force
$displayName = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('5qGM6Z2i5o+Q6YaS'))
$null = New-ItemProperty -Path $key -Name DisplayName -Value $displayName -PropertyType String -Force
$null = New-ItemProperty -Path $key -Name ShowInSettings -Value 1 -PropertyType DWord -Force
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appId)
if ($null -ne $notifier.Setting -and $notifier.Setting -ne [Windows.UI.Notifications.NotificationSetting]::Enabled) {
    throw ('NotificationsDisabled:' + $notifier.Setting)
}
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
$toast.ExpirationTime = [DateTimeOffset]::Now.AddMinutes(2)
$notifier.Show($toast)
`

func (n *windowsNotifier) Notify(parent context.Context, title, content string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if parent.Err() != nil {
		return parent.Err()
	}
	payload, err := notificationXML(title, content)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	program := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, program, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", toastScript)
	cmd.Env = append(os.Environ(), "DESKTOP_REMINDER_XML="+base64.StdEncoding.EncodeToString([]byte(payload)))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("Windows 通知发送超时，请稍后点击测试通知重试")
		}
		return ctx.Err()
	}
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if strings.Contains(detail, "NotificationsDisabled:") {
			return errors.New("Windows 已关闭通知，请在系统设置中允许“桌面提醒”发送通知")
		}
		if len(detail) > 600 {
			detail = detail[:600]
		}
		return fmt.Errorf("Windows 通知发送失败：%v %s", err, detail)
	}
	return nil
}
