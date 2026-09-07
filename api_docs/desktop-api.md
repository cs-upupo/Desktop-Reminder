# 桌面本地绑定接口

通过 Wails 注入的 `window.go.main.App` 调用，所有方法返回 Promise。仅供本机窗口使用，不是远程 HTTP API，无路由、无数据库。

## 数据结构

Config:

```json
{"content":"起身走走，喝一杯水。","intervalMinutes":30}
```

`content` 去除首尾空白后非空，最多 500 个 Unicode 字符；不接受除换行、回车、制表符外的低位控制字符。`intervalMinutes` 是 1～10080 的整数。

State:

```json
{
  "config":{"content":"起身走走，喝一杯水。","intervalMinutes":30},
  "running":false,
  "started":false,
  "remainingMs":1800000,
  "nextAt":0,
  "lastNotification":0,
  "notificationCount":0,
  "configWarning":"",
  "notificationError":""
}
```

时间戳为 Unix 毫秒。暂停时 `nextAt=0`，`remainingMs` 保留。`started` 用于区分初始与暂停状态；改变间隔后重置。通知计数包含测试通知，仅在 Windows 成功接收请求后增加；系统横幅显示取决于 Windows 设置。

## 方法

| 方法 | 参数 | 返回 | 行为 |
| --- | --- | --- | --- |
| GetState | 无 | State | 当前配置、计时、通知及错误状态 |
| SaveConfig | Config 对象 | State | 校验并原子保存；修改间隔重置倒计时；运行中禁止改配置 |
| Start | Config 对象 | State | 保存成功后开始或继续；运行中相同参数再次调用不重置定时器 |
| Pause | 无 | State | 保留剩余时间，取消尚未完成的通知请求；重复暂停安全 |
| TestNotification | Config 对象 | 无 | 校验后发送一条无声测试通知，不改变配置或计时 |
| OpenNotificationSettings | 无 | 无 | 打开本机 `ms-settings:notifications`，不修改权限或声音设置 |

失败通过 Promise rejection 返回中文错误。保存失败不会启动提醒。测试通知的界面入口在暂停状态会先调用 SaveConfig。接口调用由前端串行提交，避免保存与开始竞争。

点击叉号只隐藏至系统托盘，定时器继续工作。托盘右键“退出应用”才停止提醒并清理托盘图标；托盘初始化失败时叉号退回正常退出。应用再次启动只恢复配置，不自动开始。唤醒后只补发一次，并从当前时间开始下一轮。
