# 桌面提醒

基于 Go + Wails 的 Windows 桌面提醒工具。前端使用本地 HTML / CSS / JavaScript，无 npm 依赖，界面资源编译进 exe。

## 直接使用

双击 `build/bin/桌面提醒.exe`，或解压 `release/DesktopReminder-windows-amd64.zip` 后运行。分发包内只包含程序、使用说明和第三方许可。

- 自定义提醒内容，最多 500 个字符，支持中文与多行。
- 自定义 1～10080 的整数分钟，附带 15 / 30 / 60 分钟快捷选项。
- 开始 / 暂停 / 继续；暂停保留剩余时间，修改间隔重置为完整新间隔。
- Go 后台计时，界面只负责显示；最小化后仍工作。
- Windows 原生桌面通知，明确设置 `<audio silent="true" />`，无声音。
- 自动保存有效设置；下次启动恢复内容和间隔，默认未开始。
- 休眠后最多补发一次到期提醒，避免连续补发大量通知。
- 单实例运行，重复打开会显示已有窗口。
- 点击叉号隐藏至右下角系统托盘，计时继续；托盘菜单提供打开主窗口、系统通知设置、退出应用。
- 主界面提供“系统通知设置”按钮，直达本机通知设置页面。

从托盘右键“退出应用”停止程序；不提供开机自启动。托盘未能初始化时，叉号退回正常退出，避免出现找不到窗口的后台进程。电脑关机 / 休眠期间无法发送通知。Windows 勿扰 / 专注助手可能隐藏横幅；发送成功表示 Windows 接收了请求，不保证横幅一定显示。暂停不能撤回 Windows 已经接收的通知。

升级到 1.1.0 时请先退出旧程序，再运行新版。旧 1.0.0 点击叉号即可退出；新版在托盘右键退出。分发文件放在带版本号的目录，避免覆盖正在运行的旧程序。

## 开发与构建

当前固定 Wails **v2.15.0**，依赖 **Go 1.25.0 或更高版本**；本机已使用 Go 1.25.7 验证。自有 Go 代码不使用泛型、`any` 等高于 Go 1.14 的语言语法；框架与资源嵌入需要现代 Go 工具链，整个 Wails 项目不能使用 Go 1.14 编译。

最终用户需要 Windows 10 / 11 64 位与 Microsoft Edge WebView2 Runtime，不需要 Go、Node 或 Wails。前端没有构建依赖，Node / npm 不是必需项。

在项目目录执行：

```powershell
wails dev
wails build
```

或双击：

- `dev.bat`：检查 Go / 固定版本 Wails，运行开发窗口。
- `build.bat`：检查环境，构建 Windows amd64 版本，生成可分发 ZIP。

脚本仅对当前进程设置 `GOARCH=amd64`，不修改全局 Go 配置。Wails 缺失时会从配置的公开 Go 模块源下载固定版本。Go 缺失或低于 1.25 时会明确提示。

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
```

32 位 Go 宿主交叉安装时，Wails 位于 GOPATH 的 `bin/windows_amd64`。环境检查脚本会将其复制到 `bin/wails.exe`；本机已完成处理。构建脚本使用 `-webview2 error`，缺少 WebView2 的目标电脑显示缺少运行时提示，不自动下载安装。

产物：

```text
build/bin/桌面提醒.exe
release/windows-amd64-v1.1.0/桌面提醒.exe
release/windows-amd64-v1.1.0/README.txt
release/windows-amd64-v1.1.0/THIRD_PARTY_NOTICES.txt
release/DesktopReminder-windows-amd64.zip
```

## 实现与数据

```text
main.go                  Wails 窗口、嵌入资源、单实例
app.go                   桌面 Go / JavaScript 绑定
tray_windows.go          独立系统线程上的托盘菜单、隐藏 / 显示 / 退出
internal/reminder/       计时、配置、Windows 静音通知、核心测试
frontend/                界面与交互，无外部字体或 CDN
scripts/                 依赖检查、开发、构建、第三方许可收集
build/appicon.png        应用图标
build/tray.ico           编译进程序的托盘图标
api_docs/desktop-api.md  本地绑定接口说明
```

配置位置：`%APPDATA%/DesktopReminder/config.json`。同目录临时文件写入、同步、替换，保存失败时保留旧设置并显示错误；损坏文件复制到带时间戳的 `.invalid-*` 文件，显示默认值和告警。提醒内容不会放入分发包。

Windows 通知使用系统 PowerShell 5.1 调用 WinRT；以隐藏窗口运行、12 秒超时，固定脚本与 Base64 数据分离。提醒文本进行 XML 转义，支持特殊符号。应用只在当前用户注册表 `HKCU/Software/Classes/AppUserModelId/DesktopReminder.Silent` 注册通知名称，不要求管理员权限。不修改全局通知或声音设置。部分系统返回空通知权限状态；只有取得明确禁用状态时才阻止发送，否则继续调用 Show，以实际调用结果为准。

托盘依赖固定 `github.com/getlantern/systray v1.2.2`，Windows 无需 CGO。托盘在独立且固定的系统线程处理消息，退出时停止提醒并移除图标。“系统通知设置”按钮仅打开 `ms-settings:notifications`，不自动修改系统权限。

应用本身不请求远程服务；开发服务只绑定 `127.0.0.1:34115`。构建环境首次安装依赖时需要联网。没有数据库，不涉及 SQL。

## 本地验证

本项目没有 `controllers/v4` 或 `models/v4`，使用全部包的本地测试和编译检查：

```powershell
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
go test ./...
go test -run '^$' ./...
git diff --check
```

不访问远程业务服务。`internal/reminder/*_test.go` 使用假通知服务及可控时间检查计时行为；原生通知由桌面窗口进行手动验证。

手动验证步骤见 `VERIFICATION.md`。如需离线重复构建，可在已下载依赖后为当前终端设置 `$env:GOPROXY='off'`、`$env:GOSUMDB='off'`。
