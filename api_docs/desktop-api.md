# 桌面本地绑定接口

界面通过 Wails 注入的 `window.go.main.App` 调用这些方法。它们只在本机进程内使用，不是 HTTP 接口，不连接远程服务，也没有数据库。

## 任务数据

`TaskInput` 用于新建或编辑任务：

```json
{
  "id": "编辑时填写；新建时留空",
  "name": "提交日报",
  "content": "整理并提交今天的日报",
  "kind": "scheduled",
  "scheduleMode": "daily",
  "intervalMinutes": 0,
  "dailyTime": "17:30:00",
  "onceAt": 0,
  "repeatMinutes": 10,
  "active": true
}
```

- `kind=interval`：循环任务。使用 `intervalMinutes`，范围 1～10080 分钟；其他计划字段会被清空。首次到期后按同一间隔继续提醒，直到完成。
- `kind=scheduled`：定时任务。`scheduleMode=daily` 时使用 `dailyTime`，格式为 `HH:mm:ss`；`scheduleMode=once` 时使用 Unix 毫秒时间戳 `onceAt`。
- 定时任务首次提醒后使用 `repeatMinutes` 持续提醒，范围 1～10080 分钟，直到完成。
- `name` 去除首尾空白后非空，最多 80 个 Unicode 字符；`content` 最多 500 个字符。两者均拒绝不支持的低位控制字符。

`Task` 在上述字段外还返回运行状态：

```json
{
  "active": true,
  "awaitingCompletion": false,
  "completed": false,
  "nextAt": 1788831000000,
  "pausedRemainingMs": 0,
  "occurrenceDate": "",
  "occurrenceScheduledAt": 0,
  "lastNotification": 0,
  "lastCompletedAt": 0,
  "notificationCount": 0,
  "lastError": "",
  "createdAt": 1788829200000,
  "updatedAt": 1788829200000
}
```

所有时间戳均为 Unix 毫秒。`awaitingCompletion=true` 表示任务已首次提醒且等待用户完成；此时会继续按间隔通知。暂停会保存剩余时间。一次性任务完成后不能直接重新启用，需要先编辑新的执行时间；循环任务可重新开始；每日任务完成后自动安排到下一天。

`State`：

```json
{
  "tasks": [],
  "now": 1788829200000,
  "lastNotification": 0,
  "notificationCount": 0,
  "configWarning": ""
}
```

## 日期记录

`DayView`：

```json
{
  "date": "2026-09-08",
  "items": [
    {
      "taskId": "...",
      "name": "提交日报",
      "content": "整理并提交今天的日报",
      "kind": "scheduled",
      "scheduleMode": "daily",
      "scheduledAt": 1788869400000,
      "status": "completed",
      "completedAt": 1788869700000,
      "repeatMinutes": 10,
      "notificationAt": 1788869400000
    }
  ],
  "total": 1,
  "completed": 1,
  "pending": 0
}
```

日期格式固定为本地时区的 `YYYY-MM-DD`。状态可能为 `completed`、`pending`、`missed`、`scheduled`、`paused` 或 `running`。完成记录保存任务当时的名称和内容，因此任务后来改名或删除也不会丢失已完成历史。

## 方法

| 方法 | 参数 | 返回 | 行为 |
| --- | --- | --- | --- |
| `GetState` | 无 | `State` | 读取全部未删除任务和当前状态 |
| `SaveTask` | `TaskInput` | `State` | 新建或编辑并保存；计划改变后重新计算时间 |
| `DeleteTask` | 任务 ID | `State` | 删除任务；保留已有完成记录 |
| `StartTask` | 任务 ID | `State` | 启用或继续单个任务 |
| `PauseTask` | 任务 ID | `State` | 暂停单个任务并保留适用的剩余时间 |
| `CompleteTask` | 任务 ID | `State` | 完成已触发的任务并停止其重复通知 |
| `TestTaskNotification` | 任务 ID | 无 | 发送该任务的一条无声测试通知，不改变任务进度 |
| `GetDayView` | `YYYY-MM-DD` | `DayView` | 查看指定日期的任务和完成情况 |
| `OpenNotificationSettings` | 无 | 无 | 打开本机 Windows 通知设置 |

失败通过 Promise rejection 返回中文错误。保存采用同目录临时文件写入后替换，失败时恢复修改前的内存状态。到期状态会先持久化，再请求 Windows 通知，以减少异常退出后的重复首提醒。

窗口叉号只隐藏至系统托盘，调度继续运行；托盘右键“退出应用”才停止进程。配置和完成记录保存在 `%APPDATA%\DesktopReminder\config.json`。旧版单任务配置首次打开 2.0.0 时会自动迁移为一个暂停的循环任务。
