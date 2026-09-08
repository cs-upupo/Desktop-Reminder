# 桌面提醒

[![Windows CI](https://github.com/cs-upupo/Desktop-reminder/actions/workflows/ci.yml/badge.svg)](https://github.com/cs-upupo/Desktop-reminder/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/cs-upupo/Desktop-reminder?display_name=tag)](https://github.com/cs-upupo/Desktop-reminder/releases/latest)

基于 Go + Wails 的 Windows 多任务提醒工具。界面资源全部编译进 exe，无 npm 依赖；提醒、任务配置和完成记录仅保存在本机。

## 功能

- 同时创建并独立管理多个任务，可编辑、启用、暂停、完成、测试通知或删除。
- 循环任务：按 1～10080 分钟的固定间隔提醒。
- 每日定时任务：每天在指定的 `时:分:秒` 首次提醒。
- 一次性任务：在指定的 `年-月-日 时:分:秒` 首次提醒。
- 首次提醒后，按单独设置的间隔继续提醒，直到用户点击完成。
- 每日任务完成后自动安排到次日；一次性任务完成后结束；循环任务可重新开始。
- 按日期查看当天任务、完成数、未完成数、逐项状态和完成时间。
- 任务、倒计时、等待完成状态和历史记录自动保存，重新启动后继续恢复。
- Windows 原生静音通知；主界面可直接打开系统通知设置。
- 点击叉号隐藏到系统托盘，任务继续运行；托盘右键可恢复窗口或退出。
- 单实例运行，重复打开会显示已有窗口。

旧版 `%APPDATA%\DesktopReminder\config.json` 会自动迁移为一个暂停的循环任务。完成后再删除任务，日期记录仍会保留完成快照。

## 直接运行和分发

本机双击：

```text
build\bin\桌面提醒.exe
```

发给别人时发送整个文件：

```text
release\DesktopReminder-windows-amd64-v2.0.0.zip
```

公开版本也可从 [GitHub Releases](https://github.com/cs-upupo/Desktop-reminder/releases) 下载；每个版本同时提供 ZIP、单独的 exe 和 SHA-256 校验文件。

对方解压后双击 `桌面提醒.exe` 即可。目标电脑需要 Windows 10 / 11 64 位和 Microsoft Edge WebView2 Runtime，不需要 Go、Node 或 Wails。

## 开发与构建

项目固定使用 Wails **v2.15.0**，需要 **Go 1.25.0 或更高版本**。自有 Go 代码不使用泛型、`any` 等高于 Go 1.14 的语言语法；Wails 与 `embed` 仍要求现代 Go 工具链。

在项目目录运行：

```powershell
wails dev
wails build
```

也可以直接双击：

- `dev.bat`：检查 Go 和固定版本 Wails，然后打开开发窗口。
- `build.bat`：检查环境，构建 Windows amd64 exe，并生成包含说明和许可的 ZIP。

构建脚本只对当前进程设置 `GOARCH=amd64`，不修改全局 Go 配置。首次安装 Wails 或 Go 模块依赖时需要访问公开依赖源；依赖已有缓存后可以通过 `GOPROXY=off`、`GOSUMDB=off` 离线测试和构建。应用运行本身不联网。

## GitHub 发布维护

- 推送到 `main` 或提交 Pull Request 时，`Windows CI` 会执行前端语法检查、Go 测试和 Windows exe 构建，并保留 14 天的构建产物。
- 推送与 `wails.json` 版本一致的标签（例如 `v2.0.0`）时，`Publish Release` 会自动运行测试、生成分发包，并创建 GitHub Release。
- 每个 Release 上传 ZIP、单独的 exe 和 `SHA256SUMS.txt`；版本说明位于 `docs/releases/<版本>.md`。
- 当前产品是便携 Windows 桌面程序，不发布代码库或容器包。GitHub Packages 保留给未来确有包管理器安装需求的 NuGet 等格式；现阶段用户下载统一使用 Releases。

## 项目结构与数据

```text
main.go                  Wails 窗口、资源嵌入与单实例
app.go                   Go / JavaScript 本地绑定
tray_windows.go          系统托盘、隐藏、恢复与退出
internal/reminder/       多任务调度、配置、日期记录、静音通知与测试
frontend/                任务列表、编辑表单与日期记录界面
scripts/                 环境检查、开发、构建与许可收集
api_docs/desktop-api.md  本地绑定接口文档
```

数据位置为 `%APPDATA%\DesktopReminder\config.json`。保存采用同目录临时文件写入、同步和替换；文件损坏时会备份为 `.invalid-*` 后显示告警。应用没有数据库，不涉及 SQL。

Windows 通知通过系统 PowerShell 5.1 调用 WinRT，脚本与 Base64 数据分离，文本经过 XML 转义，并固定使用 `<audio silent="true" />`。应用只在当前用户注册表登记 `DesktopReminder.Silent`，不需要管理员权限，也不修改全局声音或通知设置。

## 验证

```powershell
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
node --check frontend/app.js
go test ./...
go test -run '^$' ./...
git diff --check
```

详细桌面验证场景见 `VERIFICATION.md`。
