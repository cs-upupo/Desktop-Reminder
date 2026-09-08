package reminder

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeNotifier struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeNotifier) Notify(ctx context.Context, title, content string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.calls = append(f.calls, title+"|"+content)
	err := f.err
	f.mu.Unlock()
	return err
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type testClock struct {
	mu    sync.Mutex
	value time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func (c *testClock) Add(value time.Duration) {
	c.mu.Lock()
	c.value = c.value.Add(value)
	c.mu.Unlock()
}

func testDirectory(t *testing.T) string {
	t.Helper()
	directory, err := ioutil.TempDir("", "desktop-reminder-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func testEngine(t *testing.T, now time.Time, notifier *fakeNotifier) (*Engine, *testClock) {
	t.Helper()
	clock := &testClock{value: now}
	engine := newEngine(filepath.Join(testDirectory(t), "config.json"), notifier, clock.Now)
	return engine, clock
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for asynchronous notification")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func taskByName(t *testing.T, state State, name string) Task {
	t.Helper()
	for i := range state.Tasks {
		if state.Tasks[i].Name == name {
			return state.Tasks[i]
		}
	}
	t.Fatalf("task %q not found", name)
	return Task{}
}

func TestMultipleTasksTriggerAndDailyCompletionAppearsInDayView(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	notifier := &fakeNotifier{}
	engine, clock := testEngine(t, time.Date(2026, 9, 8, 8, 59, 0, 0, zone), notifier)

	intervalState, err := engine.SaveTask(TaskInput{
		Name: "活动一下", Content: "站起来走一走", Kind: KindInterval,
		IntervalMinutes: 1, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	interval := taskByName(t, intervalState, "活动一下")
	_, err = engine.SaveTask(TaskInput{
		Name: "提交日报", Content: "整理并提交日报", Kind: KindScheduled,
		ScheduleMode: ScheduleDaily, DailyTime: "09:00:00", RepeatMinutes: 5, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	clock.Add(time.Minute)
	engine.checkDue()
	waitFor(t, func() bool { return notifier.count() == 2 })
	state := engine.State()
	interval = taskByName(t, state, "活动一下")
	daily := taskByName(t, state, "提交日报")
	if !interval.AwaitingCompletion || !daily.AwaitingCompletion {
		t.Fatalf("both tasks should await completion: %+v %+v", interval, daily)
	}
	if _, err = engine.CompleteTask(interval.ID); err != nil {
		t.Fatal(err)
	}
	state, err = engine.CompleteTask(daily.ID)
	if err != nil {
		t.Fatal(err)
	}
	daily = taskByName(t, state, "提交日报")
	if !daily.Active || daily.AwaitingCompletion || dateForMilliseconds(daily.NextAt, zone) != "2026-09-09" {
		t.Fatalf("daily task should wait for tomorrow: %+v", daily)
	}
	view, err := engine.DayView("2026-09-08")
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 2 || view.Completed != 2 || view.Pending != 0 {
		t.Fatalf("unexpected day view: %+v", view)
	}
}

func TestOneTimeTaskRepeatsUntilCompleted(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	notifier := &fakeNotifier{}
	start := time.Date(2026, 9, 8, 10, 0, 0, 0, zone)
	engine, clock := testEngine(t, start, notifier)
	state, err := engine.SaveTask(TaskInput{
		Name: "线上会议", Content: "进入会议室", Kind: KindScheduled,
		ScheduleMode: ScheduleOnce, OnceAt: milliseconds(start.Add(time.Minute)),
		RepeatMinutes: 1, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := taskByName(t, state, "线上会议")
	clock.Add(time.Minute)
	engine.checkDue()
	waitFor(t, func() bool { return notifier.count() == 1 })
	clock.Add(time.Minute)
	engine.checkDue()
	waitFor(t, func() bool { return notifier.count() == 2 })
	state, err = engine.CompleteTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task = taskByName(t, state, "线上会议")
	if !task.Completed || task.Active || task.AwaitingCompletion || task.NotificationCount != 2 {
		t.Fatalf("unexpected completed task: %+v", task)
	}
	if _, err = engine.StartTask(task.ID); err == nil {
		t.Fatal("completed one-time task should require a new execution time")
	}
	view, err := engine.DayView("2026-09-08")
	if err != nil || view.Completed != 1 {
		t.Fatalf("completion missing from day view: %+v, %v", view, err)
	}
}

func TestPauseResumeAndScheduleEdit(t *testing.T) {
	notifier := &fakeNotifier{}
	engine, clock := testEngine(t, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC), notifier)
	state, err := engine.SaveTask(TaskInput{
		Name: "喝水", Content: "喝一杯水", Kind: KindInterval,
		IntervalMinutes: 10, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := taskByName(t, state, "喝水")
	clock.Add(3 * time.Minute)
	state, err = engine.PauseTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task = taskByName(t, state, "喝水")
	if task.PausedRemainingMS != durationMilliseconds(7) {
		t.Fatalf("pause did not preserve seven minutes: %+v", task)
	}
	clock.Add(time.Hour)
	state, err = engine.StartTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task = taskByName(t, state, "喝水")
	expected := milliseconds(clock.Now().Add(7 * time.Minute))
	if task.NextAt != expected {
		t.Fatalf("resume changed remaining duration: got %d want %d", task.NextAt, expected)
	}

	input := taskInputFromTask(task)
	input.Content = "补充一杯温水"
	state, err = engine.SaveTask(input)
	if err != nil {
		t.Fatal(err)
	}
	task = taskByName(t, state, "喝水")
	if task.NextAt != expected {
		t.Fatal("content-only edit reset the countdown")
	}
	input = taskInputFromTask(task)
	input.IntervalMinutes = 20
	state, err = engine.SaveTask(input)
	if err != nil {
		t.Fatal(err)
	}
	task = taskByName(t, state, "喝水")
	if task.NextAt != milliseconds(clock.Now().Add(20*time.Minute)) {
		t.Fatal("schedule change did not start a full new interval")
	}
	input = taskInputFromTask(task)
	input.IntervalMinutes = 30
	input.Active = false
	state, err = engine.SaveTask(input)
	if err != nil {
		t.Fatal(err)
	}
	task = taskByName(t, state, "喝水")
	if task.Active || task.PausedRemainingMS != durationMilliseconds(30) {
		t.Fatalf("paused schedule edit did not preserve a full new interval: %+v", task)
	}
}

func TestCompletingPausedDailyTaskKeepsItPaused(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	notifier := &fakeNotifier{}
	engine, clock := testEngine(t, time.Date(2026, 9, 8, 8, 59, 0, 0, zone), notifier)
	state, err := engine.SaveTask(TaskInput{
		Name: "晨会", Content: "参加晨会", Kind: KindScheduled,
		ScheduleMode: ScheduleDaily, DailyTime: "09:00:00", RepeatMinutes: 5, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := taskByName(t, state, "晨会")
	clock.Add(time.Minute)
	engine.checkDue()
	waitFor(t, func() bool { return notifier.count() == 1 })
	if _, err = engine.PauseTask(task.ID); err != nil {
		t.Fatal(err)
	}
	state, err = engine.CompleteTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task = taskByName(t, state, "晨会")
	if task.Active || task.NextAt != 0 || task.AwaitingCompletion || task.Completed {
		t.Fatalf("paused daily task was unexpectedly re-enabled: %+v", task)
	}
}

func TestDeletingTaskPreservesCompletedDateRecord(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	notifier := &fakeNotifier{}
	engine, clock := testEngine(t, time.Date(2026, 9, 8, 14, 0, 0, 0, zone), notifier)
	state, err := engine.SaveTask(TaskInput{
		Name: "一次记录", Content: "保留完成历史", Kind: KindInterval,
		IntervalMinutes: 1, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	task := taskByName(t, state, "一次记录")
	clock.Add(time.Minute)
	engine.checkDue()
	waitFor(t, func() bool { return notifier.count() == 1 })
	if _, err = engine.CompleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	state, err = engine.DeleteTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 0 {
		t.Fatal("deleted task is still visible")
	}
	view, err := engine.DayView("2026-09-08")
	if err != nil || view.Total != 1 || view.Completed != 1 || view.Items[0].Name != "一次记录" {
		t.Fatalf("deleted completion was not preserved: %+v, %v", view, err)
	}
}

func TestLegacyConfigurationMigratesToPausedTask(t *testing.T) {
	directory := testDirectory(t)
	path := filepath.Join(directory, "config.json")
	data, err := json.Marshal(legacyConfig{Content: "旧提醒", IntervalMinutes: 30})
	if err != nil {
		t.Fatal(err)
	}
	if err = ioutil.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	clock := &testClock{value: time.Date(2026, 9, 8, 8, 0, 0, 0, time.Local)}
	engine := newEngine(path, &fakeNotifier{}, clock.Now)
	state := engine.State()
	if len(state.Tasks) != 1 || state.Tasks[0].Active || state.Tasks[0].Content != "旧提醒" || state.Tasks[0].IntervalMinutes != 30 {
		t.Fatalf("legacy migration failed: %+v", state)
	}
	if state.ConfigWarning == "" {
		t.Fatal("migration should be explained to the user")
	}
}

func TestInvalidInputsAndNotificationFailure(t *testing.T) {
	notifier := &fakeNotifier{err: errors.New("通知权限关闭")}
	engine, clock := testEngine(t, time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC), notifier)
	if _, err := engine.SaveTask(TaskInput{Name: "", Content: "内容", Kind: KindInterval, IntervalMinutes: 1}); err == nil {
		t.Fatal("empty task name was accepted")
	}
	if _, err := engine.SaveTask(TaskInput{Name: "任务", Content: "内容", Kind: KindScheduled, ScheduleMode: ScheduleDaily, DailyTime: "25:00:00", RepeatMinutes: 1}); err == nil {
		t.Fatal("invalid daily time was accepted")
	}
	state, err := engine.SaveTask(TaskInput{Name: "失败测试", Content: "通知", Kind: KindInterval, IntervalMinutes: 1, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	task := taskByName(t, state, "失败测试")
	clock.Add(time.Minute)
	engine.checkDue()
	waitFor(t, func() bool {
		state = engine.State()
		task = taskByName(t, state, "失败测试")
		return task.LastError != ""
	})
	if task.NotificationCount != 0 || !task.AwaitingCompletion {
		t.Fatalf("notification failure corrupted task state: %+v", task)
	}
}
