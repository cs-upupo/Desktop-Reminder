package reminder

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	StoreVersion       = 2
	KindInterval       = "interval"
	KindScheduled      = "scheduled"
	ScheduleDaily      = "daily"
	ScheduleOnce       = "once"
	maxIntervalMinutes = 10080
)

type TaskInput struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Content         string `json:"content"`
	Kind            string `json:"kind"`
	ScheduleMode    string `json:"scheduleMode"`
	IntervalMinutes int    `json:"intervalMinutes"`
	DailyTime       string `json:"dailyTime"`
	OnceAt          int64  `json:"onceAt"`
	RepeatMinutes   int    `json:"repeatMinutes"`
	Active          bool   `json:"active"`
}

type Task struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Content               string `json:"content"`
	Kind                  string `json:"kind"`
	ScheduleMode          string `json:"scheduleMode"`
	IntervalMinutes       int    `json:"intervalMinutes"`
	DailyTime             string `json:"dailyTime"`
	OnceAt                int64  `json:"onceAt"`
	RepeatMinutes         int    `json:"repeatMinutes"`
	Active                bool   `json:"active"`
	AwaitingCompletion    bool   `json:"awaitingCompletion"`
	Completed             bool   `json:"completed"`
	NextAt                int64  `json:"nextAt"`
	PausedRemainingMS     int64  `json:"pausedRemainingMs"`
	OccurrenceDate        string `json:"occurrenceDate"`
	OccurrenceScheduledAt int64  `json:"occurrenceScheduledAt"`
	LastNotification      int64  `json:"lastNotification"`
	LastCompletedAt       int64  `json:"lastCompletedAt"`
	NotificationCount     int    `json:"notificationCount"`
	LastError             string `json:"lastError"`
	CreatedAt             int64  `json:"createdAt"`
	UpdatedAt             int64  `json:"updatedAt"`
	DeletedAt             int64  `json:"deletedAt"`
}

type CompletionRecord struct {
	TaskID       string `json:"taskId"`
	Date         string `json:"date"`
	CompletedAt  int64  `json:"completedAt"`
	Name         string `json:"name"`
	Content      string `json:"content"`
	Kind         string `json:"kind"`
	ScheduleMode string `json:"scheduleMode"`
	ScheduledAt  int64  `json:"scheduledAt"`
}

type Store struct {
	Version           int                `json:"version"`
	Tasks             []Task             `json:"tasks"`
	Completions       []CompletionRecord `json:"completions"`
	LastNotification  int64              `json:"lastNotification"`
	NotificationCount int                `json:"notificationCount"`
}

type legacyConfig struct {
	Content         string `json:"content"`
	IntervalMinutes int    `json:"intervalMinutes"`
}

func defaultStore() Store {
	return Store{Version: StoreVersion, Tasks: []Task{}, Completions: []CompletionRecord{}}
}

func DefaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "DesktopReminder", "config.json")
}

func normalizeTaskInput(input TaskInput) (TaskInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Content = strings.TrimSpace(input.Content)
	input.Kind = strings.TrimSpace(input.Kind)
	input.ScheduleMode = strings.TrimSpace(input.ScheduleMode)
	input.DailyTime = strings.TrimSpace(input.DailyTime)

	if err := validateText(input.Name, 80, "请输入任务名称", "任务名称最多 80 个字符"); err != nil {
		return input, err
	}
	if err := validateText(input.Content, 500, "请输入提醒内容", "提醒内容最多 500 个字符"); err != nil {
		return input, err
	}

	switch input.Kind {
	case KindInterval:
		if err := validateMinutes(input.IntervalMinutes, "循环间隔"); err != nil {
			return input, err
		}
		input.ScheduleMode = ""
		input.DailyTime = ""
		input.OnceAt = 0
		input.RepeatMinutes = input.IntervalMinutes
	case KindScheduled:
		if err := validateMinutes(input.RepeatMinutes, "再次提醒间隔"); err != nil {
			return input, err
		}
		input.IntervalMinutes = 0
		switch input.ScheduleMode {
		case ScheduleDaily:
			if _, err := parseDailyTime(input.DailyTime); err != nil {
				return input, errors.New("每日提醒时间格式不正确")
			}
			input.OnceAt = 0
		case ScheduleOnce:
			if input.OnceAt <= 0 {
				return input, errors.New("请选择一次性任务的执行日期和时间")
			}
			input.DailyTime = ""
		default:
			return input, errors.New("请选择每日执行或指定日期执行")
		}
	default:
		return input, errors.New("请选择循环任务或定时任务")
	}
	return input, nil
}

func validateText(value string, max int, emptyMessage, longMessage string) error {
	if value == "" {
		return errors.New(emptyMessage)
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return errors.New(longMessage)
	}
	for _, r := range value {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			return errors.New("文字内容含不支持的控制字符")
		}
	}
	return nil
}

func validateMinutes(value int, label string) error {
	if value < 1 || value > maxIntervalMinutes {
		return fmt.Errorf("%s必须是 1～%d 之间的整数分钟", label, maxIntervalMinutes)
	}
	return nil
}

func parseDailyTime(value string) (time.Time, error) {
	return time.Parse("15:04:05", value)
}

func generateID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("生成任务编号失败：%w", err)
	}
	return hex.EncodeToString(data), nil
}

func taskInputFromTask(task Task) TaskInput {
	return TaskInput{
		ID: task.ID, Name: task.Name, Content: task.Content, Kind: task.Kind,
		ScheduleMode: task.ScheduleMode, IntervalMinutes: task.IntervalMinutes,
		DailyTime: task.DailyTime, OnceAt: task.OnceAt,
		RepeatMinutes: task.RepeatMinutes, Active: task.Active,
	}
}

func validateStoredTask(task Task) error {
	if task.ID == "" {
		return errors.New("任务编号为空")
	}
	if _, err := normalizeTaskInput(taskInputFromTask(task)); err != nil {
		return err
	}
	if task.NotificationCount < 0 || task.NextAt < 0 || task.PausedRemainingMS < 0 {
		return errors.New("任务运行状态无效")
	}
	if task.Completed && task.Active {
		return errors.New("已完成任务不能同时处于启用状态")
	}
	return nil
}

func loadStore(path string, now time.Time) (Store, string, bool) {
	store := defaultStore()
	if path == "" {
		return store, "无法定位用户配置目录，暂时不能保存任务", false
	}
	data, err := ioutil.ReadFile(path)
	if os.IsNotExist(err) {
		return store, "", false
	}
	if err != nil {
		return store, fmt.Sprintf("读取任务数据失败：%v", err), false
	}

	var probe struct {
		Version int `json:"version"`
	}
	if err = json.Unmarshal(data, &probe); err == nil && probe.Version == StoreVersion {
		if err = json.Unmarshal(data, &store); err == nil {
			seen := make(map[string]bool)
			for i := range store.Tasks {
				if err = validateStoredTask(store.Tasks[i]); err != nil {
					break
				}
				if seen[store.Tasks[i].ID] {
					err = errors.New("存在重复的任务编号")
					break
				}
				seen[store.Tasks[i].ID] = true
			}
		}
		if err == nil {
			if store.Tasks == nil {
				store.Tasks = []Task{}
			}
			if store.Completions == nil {
				store.Completions = []CompletionRecord{}
			}
			return store, "", false
		}
	}

	var legacy legacyConfig
	if legacyErr := json.Unmarshal(data, &legacy); legacyErr == nil {
		legacyInput := TaskInput{Name: "原有循环提醒", Content: legacy.Content, Kind: KindInterval, IntervalMinutes: legacy.IntervalMinutes}
		if legacyInput, legacyErr = normalizeTaskInput(legacyInput); legacyErr == nil {
			id, idErr := generateID()
			if idErr == nil {
				nowMS := milliseconds(now)
				store.Tasks = append(store.Tasks, Task{
					ID: id, Name: legacyInput.Name, Content: legacyInput.Content,
					Kind: KindInterval, IntervalMinutes: legacyInput.IntervalMinutes,
					RepeatMinutes:     legacyInput.IntervalMinutes,
					PausedRemainingMS: durationMilliseconds(legacyInput.IntervalMinutes),
					CreatedAt:         nowMS, UpdatedAt: nowMS,
				})
				return store, "已将旧版单任务设置迁移为一个暂停的循环任务", true
			}
		}
	}

	backup := path + ".invalid-" + time.Now().Format("20060102-150405.000000000")
	if backupErr := ioutil.WriteFile(backup, data, 0600); backupErr != nil {
		return store, fmt.Sprintf("任务数据损坏且备份失败，已显示空任务列表：%v", backupErr), false
	}
	return store, "任务数据无法读取，原文件已备份，当前显示空任务列表", false
}

// 先完整写入同目录临时文件，再替换配置；保存失败时调用方恢复内存中的旧状态。
func saveStore(path string, store Store) error {
	if path == "" {
		return errors.New("无法定位用户配置目录")
	}
	store.Version = StoreVersion
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := ioutil.TempFile(filepath.Dir(path), ".tasks-*.tmp")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
