package reminder

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Notifier interface {
	Notify(context.Context, string, string) error
}

type State struct {
	Tasks             []Task `json:"tasks"`
	Now               int64  `json:"now"`
	LastNotification  int64  `json:"lastNotification"`
	NotificationCount int    `json:"notificationCount"`
	ConfigWarning     string `json:"configWarning"`
}

type DayTask struct {
	TaskID         string `json:"taskId"`
	Name           string `json:"name"`
	Content        string `json:"content"`
	Kind           string `json:"kind"`
	ScheduleMode   string `json:"scheduleMode"`
	ScheduledAt    int64  `json:"scheduledAt"`
	Status         string `json:"status"`
	CompletedAt    int64  `json:"completedAt"`
	RepeatMinutes  int    `json:"repeatMinutes"`
	NotificationAt int64  `json:"notificationAt"`
}

type DayView struct {
	Date      string    `json:"date"`
	Items     []DayTask `json:"items"`
	Total     int       `json:"total"`
	Completed int       `json:"completed"`
	Pending   int       `json:"pending"`
}

type delivery struct {
	cancel context.CancelFunc
}

type dueNotification struct {
	taskID  string
	title   string
	content string
}

type Engine struct {
	mu       sync.Mutex
	store    Store
	path     string
	notifier Notifier
	now      func() time.Time
	warning  string

	ctx       context.Context
	cancel    context.CancelFunc
	active    map[string]*delivery
	runOnce   sync.Once
	closeOnce sync.Once
	done      chan struct{}
}

func NewEngine(path string, notifier Notifier) *Engine {
	return newEngine(path, notifier, time.Now)
}

func newEngine(path string, notifier Notifier, now func() time.Time) *Engine {
	if now == nil {
		now = time.Now
	}
	store, warning, migrated := loadStore(path, now())
	engine := &Engine{
		store: store, path: path, notifier: notifier, now: now, warning: warning,
		active: make(map[string]*delivery), done: make(chan struct{}),
	}
	changed := engine.prepareLoadedTasks(now())
	if migrated || changed {
		if err := saveStore(path, engine.store); err != nil {
			engine.warning = appendWarning(engine.warning, "保存迁移后的任务数据失败："+err.Error())
		}
	}
	return engine
}

func appendWarning(current, extra string) string {
	if current == "" {
		return extra
	}
	return current + "；" + extra
}

func (e *Engine) prepareLoadedTasks(now time.Time) bool {
	changed := false
	nowMS := milliseconds(now)
	for i := range e.store.Tasks {
		task := &e.store.Tasks[i]
		if task.CreatedAt == 0 {
			task.CreatedAt = nowMS
			changed = true
		}
		if task.UpdatedAt == 0 {
			task.UpdatedAt = task.CreatedAt
			changed = true
		}
		if task.DeletedAt != 0 {
			if task.Active || task.NextAt != 0 {
				task.Active = false
				task.NextAt = 0
				changed = true
			}
			continue
		}
		if task.Completed {
			if task.Active || task.NextAt != 0 {
				task.Active = false
				task.NextAt = 0
				changed = true
			}
			continue
		}
		if task.Active {
			if task.NextAt == 0 {
				if task.AwaitingCompletion {
					task.NextAt = nowMS + remainingOrDefault(task.PausedRemainingMS, task.RepeatMinutes)
				} else {
					scheduleInitial(task, now)
				}
				task.PausedRemainingMS = 0
				changed = true
			}
			continue
		}
		if task.NextAt > 0 {
			if task.Kind == KindInterval || task.AwaitingCompletion {
				task.PausedRemainingMS = task.NextAt - nowMS
				if task.PausedRemainingMS < 1000 {
					task.PausedRemainingMS = 1000
				}
			}
			task.NextAt = 0
			changed = true
		}
		if task.Kind == KindInterval && task.PausedRemainingMS == 0 {
			task.PausedRemainingMS = durationMilliseconds(task.IntervalMinutes)
			changed = true
		}
	}
	return changed
}

func (e *Engine) Run(parent context.Context) {
	e.runOnce.Do(func() {
		if parent == nil {
			parent = context.Background()
		}
		e.mu.Lock()
		e.ctx, e.cancel = context.WithCancel(parent)
		e.mu.Unlock()
		go e.loop()
	})
}

func (e *Engine) loop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	defer close(e.done)
	e.mu.Lock()
	done := e.ctx.Done()
	e.mu.Unlock()
	for {
		select {
		case <-ticker.C:
			e.checkDue()
		case <-done:
			return
		}
	}
}

func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		cancel := e.cancel
		for _, item := range e.active {
			item.cancel()
		}
		e.active = make(map[string]*delivery)
		e.mu.Unlock()
		if cancel == nil {
			return
		}
		cancel()
		<-e.done
	})
}

func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateLocked(e.now())
}

func (e *Engine) stateLocked(now time.Time) State {
	tasks := make([]Task, 0, len(e.store.Tasks))
	for i := range e.store.Tasks {
		if e.store.Tasks[i].DeletedAt == 0 {
			tasks = append(tasks, e.store.Tasks[i])
		}
	}
	return State{
		Tasks: tasks, Now: milliseconds(now), LastNotification: e.store.LastNotification,
		NotificationCount: e.store.NotificationCount, ConfigWarning: e.warning,
	}
}

func (e *Engine) SaveTask(input TaskInput) (State, error) {
	normalized, err := normalizeTaskInput(input)
	if err != nil {
		return e.State(), err
	}
	now := e.now()
	nowMS := milliseconds(now)

	e.mu.Lock()
	defer e.mu.Unlock()
	before := cloneStore(e.store)
	cancelDelivery := false

	if normalized.ID == "" {
		id, idErr := generateID()
		if idErr != nil {
			return e.stateLocked(now), idErr
		}
		task := Task{
			ID: id, Name: normalized.Name, Content: normalized.Content, Kind: normalized.Kind,
			ScheduleMode: normalized.ScheduleMode, IntervalMinutes: normalized.IntervalMinutes,
			DailyTime: normalized.DailyTime, OnceAt: normalized.OnceAt,
			RepeatMinutes: normalized.RepeatMinutes, CreatedAt: nowMS, UpdatedAt: nowMS,
		}
		if normalized.Active {
			if err = activateTask(&task, now); err != nil {
				return e.stateLocked(now), err
			}
		} else if task.Kind == KindInterval {
			task.PausedRemainingMS = durationMilliseconds(task.IntervalMinutes)
		}
		e.store.Tasks = append([]Task{task}, e.store.Tasks...)
	} else {
		task := findTask(e.store.Tasks, normalized.ID)
		if task == nil {
			return e.stateLocked(now), errors.New("没有找到要编辑的任务")
		}
		definitionChanged := task.Kind != normalized.Kind ||
			task.ScheduleMode != normalized.ScheduleMode ||
			task.IntervalMinutes != normalized.IntervalMinutes ||
			task.DailyTime != normalized.DailyTime || task.OnceAt != normalized.OnceAt
		repeatChanged := task.RepeatMinutes != normalized.RepeatMinutes
		wasActive := task.Active
		if task.Completed && task.Kind == KindScheduled && task.ScheduleMode == ScheduleOnce &&
			normalized.Active && !definitionChanged {
			return e.stateLocked(now), errors.New("一次性任务已完成，请先修改执行时间再启用")
		}

		task.Name = normalized.Name
		task.Content = normalized.Content
		task.Kind = normalized.Kind
		task.ScheduleMode = normalized.ScheduleMode
		task.IntervalMinutes = normalized.IntervalMinutes
		task.DailyTime = normalized.DailyTime
		task.OnceAt = normalized.OnceAt
		task.RepeatMinutes = normalized.RepeatMinutes
		task.UpdatedAt = nowMS

		if definitionChanged {
			cancelDelivery = task.AwaitingCompletion
			task.Active = false
			task.AwaitingCompletion = false
			task.Completed = false
			task.NextAt = 0
			task.PausedRemainingMS = 0
			task.OccurrenceDate = ""
			task.OccurrenceScheduledAt = 0
			task.LastError = ""
			if task.Kind == KindInterval {
				task.PausedRemainingMS = durationMilliseconds(task.IntervalMinutes)
			}
		} else if repeatChanged && task.AwaitingCompletion {
			task.PausedRemainingMS = durationMilliseconds(task.RepeatMinutes)
		}
		if normalized.Active {
			if definitionChanged || !wasActive {
				if err = activateTask(task, now); err != nil {
					e.store = before
					return e.stateLocked(now), err
				}
			} else if repeatChanged && task.AwaitingCompletion {
				task.NextAt = nowMS + durationMilliseconds(task.RepeatMinutes)
				task.PausedRemainingMS = 0
			}
		} else {
			if task.Active {
				pauseRuntime(task, now)
			}
			task.Active = false
			task.NextAt = 0
		}
	}

	if err = e.saveLocked(before); err != nil {
		return e.stateLocked(now), err
	}
	if cancelDelivery {
		e.cancelDeliveryLocked(normalized.ID)
	}
	return e.stateLocked(now), nil
}

func (e *Engine) DeleteTask(id string) (State, error) {
	id = strings.TrimSpace(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	task := findTask(e.store.Tasks, id)
	if task == nil {
		return e.stateLocked(now), errors.New("没有找到要删除的任务")
	}
	before := cloneStore(e.store)
	task.Active = false
	task.NextAt = 0
	task.DeletedAt = milliseconds(now)
	task.UpdatedAt = task.DeletedAt
	if err := e.saveLocked(before); err != nil {
		return e.stateLocked(now), err
	}
	e.cancelDeliveryLocked(id)
	return e.stateLocked(now), nil
}

func (e *Engine) StartTask(id string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	task := findTask(e.store.Tasks, strings.TrimSpace(id))
	if task == nil {
		return e.stateLocked(now), errors.New("没有找到要启用的任务")
	}
	if task.Active {
		return e.stateLocked(now), nil
	}
	if task.Completed && task.Kind == KindScheduled && task.ScheduleMode == ScheduleOnce {
		return e.stateLocked(now), errors.New("一次性任务已完成，请编辑新的执行时间")
	}
	before := cloneStore(e.store)
	if err := activateTask(task, now); err != nil {
		e.store = before
		return e.stateLocked(now), err
	}
	task.UpdatedAt = milliseconds(now)
	if err := e.saveLocked(before); err != nil {
		return e.stateLocked(now), err
	}
	return e.stateLocked(now), nil
}

func (e *Engine) PauseTask(id string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	task := findTask(e.store.Tasks, strings.TrimSpace(id))
	if task == nil {
		return e.stateLocked(now), errors.New("没有找到要暂停的任务")
	}
	if !task.Active {
		return e.stateLocked(now), nil
	}
	before := cloneStore(e.store)
	pauseRuntime(task, now)
	task.UpdatedAt = milliseconds(now)
	if err := e.saveLocked(before); err != nil {
		return e.stateLocked(now), err
	}
	e.cancelDeliveryLocked(id)
	return e.stateLocked(now), nil
}

func (e *Engine) CompleteTask(id string) (State, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	task := findTask(e.store.Tasks, strings.TrimSpace(id))
	if task == nil {
		return e.stateLocked(now), errors.New("没有找到要完成的任务")
	}
	if !task.AwaitingCompletion {
		return e.stateLocked(now), errors.New("任务尚未触发提醒，无需标记完成")
	}
	before := cloneStore(e.store)
	nowMS := milliseconds(now)
	wasActive := task.Active
	recordCompletion(&e.store, *task, nowMS, now.Location())
	task.AwaitingCompletion = false
	task.OccurrenceDate = ""
	task.OccurrenceScheduledAt = 0
	task.PausedRemainingMS = 0
	task.LastCompletedAt = nowMS
	task.LastError = ""
	if task.Kind == KindScheduled && task.ScheduleMode == ScheduleDaily {
		task.Active = wasActive
		task.Completed = false
		if wasActive {
			task.NextAt = milliseconds(nextDaily(now, task.DailyTime))
		} else {
			task.NextAt = 0
		}
	} else {
		task.Active = false
		task.Completed = true
		task.NextAt = 0
	}
	task.UpdatedAt = nowMS
	if err := e.saveLocked(before); err != nil {
		return e.stateLocked(now), err
	}
	e.cancelDeliveryLocked(id)
	return e.stateLocked(now), nil
}

func (e *Engine) TestTaskNotification(id string) error {
	id = strings.TrimSpace(id)
	e.mu.Lock()
	task := findTask(e.store.Tasks, id)
	if task == nil {
		e.mu.Unlock()
		return errors.New("没有找到要测试的任务")
	}
	title, content := "桌面提醒 · "+task.Name, task.Content
	parent := e.ctx
	if parent == nil {
		parent = context.Background()
	}
	e.mu.Unlock()

	if e.notifier == nil {
		return errors.New("通知组件不可用")
	}
	err := e.notifier.Notify(parent, title, content)

	e.mu.Lock()
	defer e.mu.Unlock()
	task = findTask(e.store.Tasks, id)
	if task == nil {
		return err
	}
	before := cloneStore(e.store)
	if err != nil {
		task.LastError = err.Error()
	} else {
		nowMS := milliseconds(e.now())
		task.LastError = ""
		task.LastNotification = nowMS
		task.NotificationCount++
		e.store.LastNotification = nowMS
		e.store.NotificationCount++
	}
	if saveErr := e.saveLocked(before); saveErr != nil && err == nil {
		return saveErr
	}
	return err
}

func (e *Engine) DayView(date string) (DayView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	location := now.Location()
	date = strings.TrimSpace(date)
	day, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return DayView{}, errors.New("日期格式不正确")
	}
	end := day.AddDate(0, 0, 1)
	today := startOfDay(now)
	records := make(map[string]CompletionRecord)
	for i := range e.store.Completions {
		record := e.store.Completions[i]
		if record.Date == date {
			previous, exists := records[record.TaskID]
			if !exists || record.CompletedAt > previous.CompletedAt {
				records[record.TaskID] = record
			}
		}
	}

	view := DayView{Date: date, Items: []DayTask{}}
	seen := make(map[string]bool)
	for i := range e.store.Tasks {
		task := e.store.Tasks[i]
		if task.DeletedAt != 0 || !taskExistsOnDay(task, day, end) {
			continue
		}
		record, completed := records[task.ID]
		item, include := dayTaskFor(task, record, completed, day, end, today, now)
		if !include {
			continue
		}
		view.Items = append(view.Items, item)
		seen[task.ID] = true
	}
	for taskID, record := range records {
		if seen[taskID] {
			continue
		}
		view.Items = append(view.Items, DayTask{
			TaskID: taskID, Name: record.Name, Content: record.Content, Kind: record.Kind,
			ScheduleMode: record.ScheduleMode, ScheduledAt: record.ScheduledAt,
			Status: "completed", CompletedAt: record.CompletedAt,
		})
	}
	sort.SliceStable(view.Items, func(i, j int) bool {
		left, right := view.Items[i], view.Items[j]
		if left.ScheduledAt == 0 && right.ScheduledAt != 0 {
			return false
		}
		if left.ScheduledAt != 0 && right.ScheduledAt == 0 {
			return true
		}
		if left.ScheduledAt != right.ScheduledAt {
			return left.ScheduledAt < right.ScheduledAt
		}
		return left.Name < right.Name
	})
	view.Total = len(view.Items)
	for i := range view.Items {
		if view.Items[i].Status == "completed" {
			view.Completed++
		} else {
			view.Pending++
		}
	}
	return view, nil
}

func dayTaskFor(task Task, record CompletionRecord, completed bool, day, end, today, now time.Time) (DayTask, bool) {
	item := DayTask{
		TaskID: task.ID, Name: task.Name, Content: task.Content, Kind: task.Kind,
		ScheduleMode: task.ScheduleMode, RepeatMinutes: task.RepeatMinutes,
		NotificationAt: task.LastNotification,
	}
	if completed {
		item.Name = record.Name
		item.Content = record.Content
		item.Kind = record.Kind
		item.ScheduleMode = record.ScheduleMode
		item.ScheduledAt = record.ScheduledAt
		item.Status = "completed"
		item.CompletedAt = record.CompletedAt
		return item, true
	}

	date := day.Format("2006-01-02")
	pastDay := day.Before(today)
	futureDay := day.After(today)
	if task.Completed && task.LastCompletedAt > 0 && !day.Before(startOfDay(timeFromMilliseconds(task.LastCompletedAt).In(now.Location()))) {
		return DayTask{}, false
	}
	if task.AwaitingCompletion && task.OccurrenceDate != "" {
		if task.OccurrenceDate == date {
			item.Status = "pending"
			item.ScheduledAt = task.OccurrenceScheduledAt
			return item, true
		}
		if date > task.OccurrenceDate {
			return DayTask{}, false
		}
	}

	switch task.Kind {
	case KindScheduled:
		if task.ScheduleMode == ScheduleOnce {
			scheduled := timeFromMilliseconds(task.OnceAt).In(now.Location())
			if scheduled.Before(day) || !scheduled.Before(end) {
				return DayTask{}, false
			}
			item.ScheduledAt = task.OnceAt
		} else {
			item.ScheduledAt = milliseconds(timeOnDay(day, task.DailyTime))
		}
		if !task.Active {
			item.Status = "paused"
		} else if pastDay || (!futureDay && item.ScheduledAt <= milliseconds(now)) {
			item.Status = "missed"
		} else {
			item.Status = "scheduled"
		}
	case KindInterval:
		if pastDay {
			item.Status = "missed"
		} else if !task.Active {
			item.Status = "paused"
		} else if futureDay {
			item.Status = "scheduled"
		} else {
			item.Status = "running"
			item.ScheduledAt = task.NextAt
		}
	default:
		return DayTask{}, false
	}
	return item, true
}

func taskExistsOnDay(task Task, day, end time.Time) bool {
	dayStartMS := milliseconds(day)
	dayEndMS := milliseconds(end)
	if task.CreatedAt >= dayEndMS {
		return false
	}
	if task.AwaitingCompletion && task.OccurrenceDate == day.Format("2006-01-02") {
		return true
	}
	if task.Kind == KindScheduled && task.ScheduleMode == ScheduleOnce {
		return task.OnceAt >= dayStartMS && task.OnceAt < dayEndMS
	}
	if task.Kind == KindScheduled && task.ScheduleMode == ScheduleDaily {
		return milliseconds(timeOnDay(day, task.DailyTime)) >= task.CreatedAt
	}
	return true
}

func (e *Engine) checkDue() {
	now := e.now()
	nowMS := milliseconds(now)
	e.mu.Lock()
	before := cloneStore(e.store)
	items := make([]dueNotification, 0)
	for i := range e.store.Tasks {
		task := &e.store.Tasks[i]
		if task.DeletedAt != 0 || !task.Active || task.Completed || task.NextAt == 0 || task.NextAt > nowMS {
			continue
		}
		dueAt := task.NextAt
		if !task.AwaitingCompletion {
			task.AwaitingCompletion = true
			task.OccurrenceScheduledAt = dueAt
			task.OccurrenceDate = dateForMilliseconds(dueAt, now.Location())
		}
		task.NextAt = nowMS + durationMilliseconds(task.RepeatMinutes)
		task.PausedRemainingMS = 0
		task.LastError = ""
		task.UpdatedAt = nowMS
		items = append(items, dueNotification{taskID: task.ID, title: "桌面提醒 · " + task.Name, content: task.Content})
	}
	if len(items) == 0 {
		e.mu.Unlock()
		return
	}
	if err := saveStore(e.path, e.store); err != nil {
		e.store = before
		e.warning = appendWarning(e.warning, "保存提醒状态失败："+err.Error())
		e.mu.Unlock()
		return
	}
	e.warning = ""
	e.mu.Unlock()

	for i := range items {
		e.deliver(items[i])
	}
}

func (e *Engine) deliver(item dueNotification) {
	e.mu.Lock()
	if e.ctx != nil && e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	if previous := e.active[item.taskID]; previous != nil {
		previous.cancel()
	}
	parent := e.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	current := &delivery{cancel: cancel}
	e.active[item.taskID] = current
	e.mu.Unlock()

	go func() {
		var err error
		if e.notifier == nil {
			err = errors.New("通知组件不可用")
		} else {
			err = e.notifier.Notify(ctx, item.title, item.content)
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.active[item.taskID] == current {
			delete(e.active, item.taskID)
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		task := findTask(e.store.Tasks, item.taskID)
		if task == nil {
			return
		}
		before := cloneStore(e.store)
		nowMS := milliseconds(e.now())
		if err != nil {
			task.LastError = err.Error()
		} else {
			task.LastError = ""
			task.LastNotification = nowMS
			task.NotificationCount++
			e.store.LastNotification = nowMS
			e.store.NotificationCount++
		}
		task.UpdatedAt = nowMS
		if saveErr := saveStore(e.path, e.store); saveErr != nil {
			e.store = before
			e.warning = appendWarning(e.warning, "保存通知结果失败："+saveErr.Error())
		} else {
			e.warning = ""
		}
	}()
}

func (e *Engine) saveLocked(before Store) error {
	if err := saveStore(e.path, e.store); err != nil {
		e.store = before
		e.warning = appendWarning(e.warning, "保存任务失败："+err.Error())
		return fmt.Errorf("保存任务失败：%w", err)
	}
	e.warning = ""
	return nil
}

func (e *Engine) cancelDeliveryLocked(id string) {
	if item := e.active[id]; item != nil {
		item.cancel()
		delete(e.active, id)
	}
}

func findTask(tasks []Task, id string) *Task {
	for i := range tasks {
		if tasks[i].ID == id && tasks[i].DeletedAt == 0 {
			return &tasks[i]
		}
	}
	return nil
}

func cloneStore(store Store) Store {
	copyStore := store
	copyStore.Tasks = append([]Task(nil), store.Tasks...)
	copyStore.Completions = append([]CompletionRecord(nil), store.Completions...)
	return copyStore
}

func activateTask(task *Task, now time.Time) error {
	if task.Completed && task.Kind == KindScheduled && task.ScheduleMode == ScheduleOnce {
		return errors.New("一次性任务已完成，请编辑新的执行时间")
	}
	task.Active = true
	task.Completed = false
	if task.AwaitingCompletion {
		task.NextAt = milliseconds(now) + remainingOrDefault(task.PausedRemainingMS, task.RepeatMinutes)
		task.PausedRemainingMS = 0
		return nil
	}
	scheduleInitial(task, now)
	task.PausedRemainingMS = 0
	return nil
}

func scheduleInitial(task *Task, now time.Time) {
	switch task.Kind {
	case KindInterval:
		remaining := remainingOrDefault(task.PausedRemainingMS, task.IntervalMinutes)
		task.NextAt = milliseconds(now) + remaining
	case KindScheduled:
		if task.ScheduleMode == ScheduleDaily {
			task.NextAt = milliseconds(nextDaily(now, task.DailyTime))
		} else if task.OnceAt <= milliseconds(now) {
			task.NextAt = milliseconds(now)
		} else {
			task.NextAt = task.OnceAt
		}
	}
}

func pauseRuntime(task *Task, now time.Time) {
	if task.Kind == KindInterval || task.AwaitingCompletion {
		remaining := task.NextAt - milliseconds(now)
		if remaining < 1000 {
			remaining = 1000
		}
		task.PausedRemainingMS = remaining
	} else {
		task.PausedRemainingMS = 0
	}
	task.Active = false
	task.NextAt = 0
}

func recordCompletion(store *Store, task Task, completedAt int64, location *time.Location) {
	date := task.OccurrenceDate
	if date == "" {
		date = dateForMilliseconds(completedAt, location)
	}
	record := CompletionRecord{
		TaskID: task.ID, Date: date, CompletedAt: completedAt, Name: task.Name,
		Content: task.Content, Kind: task.Kind, ScheduleMode: task.ScheduleMode,
		ScheduledAt: task.OccurrenceScheduledAt,
	}
	for i := range store.Completions {
		if store.Completions[i].TaskID == task.ID && store.Completions[i].Date == date {
			store.Completions[i] = record
			return
		}
	}
	store.Completions = append(store.Completions, record)
}

func remainingOrDefault(remaining int64, minutes int) int64 {
	if remaining > 0 {
		return remaining
	}
	return durationMilliseconds(minutes)
}

func durationMilliseconds(minutes int) int64 {
	return int64(minutes) * int64(time.Minute/time.Millisecond)
}

func milliseconds(value time.Time) int64 {
	return value.UnixNano() / int64(time.Millisecond)
}

func timeFromMilliseconds(value int64) time.Time {
	return time.Unix(0, value*int64(time.Millisecond))
}

func dateForMilliseconds(value int64, location *time.Location) string {
	return timeFromMilliseconds(value).In(location).Format("2006-01-02")
}

func startOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func timeOnDay(day time.Time, dailyTime string) time.Time {
	clock, _ := parseDailyTime(dailyTime)
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, day.Location())
}

func nextDaily(now time.Time, dailyTime string) time.Time {
	candidate := timeOnDay(now, dailyTime)
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}
