package reminder

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingNotifier struct {
	messages []string
	err      error
}

func (n *recordingNotifier) Notify(ctx context.Context, title, content string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	n.messages = append(n.messages, content)
	return n.err
}

func fixture(t *testing.T) (*Engine, *recordingNotifier, *time.Time) {
	t.Helper()
	dir, err := ioutil.TempDir("", "desktop-reminder-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	n := &recordingNotifier{}
	e := NewEngine(filepath.Join(dir, "config.json"), n)
	t.Cleanup(e.Close)
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.Local)
	e.now = func() time.Time { return now }
	return e, n, &now
}

func TestPauseResumeAndNoDuplicateTimers(t *testing.T) {
	e, n, now := fixture(t)
	cfg := Config{Content: "喝水", IntervalMinutes: 1}
	if _, err := e.Start(cfg); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(20 * time.Second)
	state, err := e.Start(cfg)
	if err != nil || state.RemainingMS != 40000 {
		t.Fatalf("double start reset timer: %+v %v", state, err)
	}
	state = e.Pause()
	if state.Running || state.RemainingMS != 40000 {
		t.Fatalf("pause: %+v", state)
	}
	*now = now.Add(3 * time.Minute)
	e.checkDue()
	if len(n.messages) != 0 || e.State().RemainingMS != 40000 {
		t.Fatal("paused reminder advanced")
	}
	if _, err := e.Start(cfg); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(39 * time.Second)
	e.checkDue()
	if len(n.messages) != 0 {
		t.Fatal("reminder arrived early")
	}
	*now = now.Add(time.Second)
	e.checkDue()
	e.checkDue()
	if len(n.messages) != 1 || e.State().RemainingMS != 60000 {
		t.Fatal("deadline must send exactly once and repeat")
	}
}

func TestSleepWakeCoalescesMissedReminders(t *testing.T) {
	e, n, now := fixture(t)
	if _, err := e.Start(Config{Content: "休息", IntervalMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(12 * time.Hour)
	for i := 0; i < 5; i++ {
		e.checkDue()
	}
	if len(n.messages) != 1 || e.State().RemainingMS != 60000 {
		t.Fatal("wake must send one notification, without backlog")
	}
}

func TestConfigurationRestoresPausedAndIntervalChangesReset(t *testing.T) {
	e, _, now := fixture(t)
	cfg := Config{Content: "中文 & <文本> 😀", IntervalMinutes: 1}
	if _, err := e.Start(cfg); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(20 * time.Second)
	e.Pause()
	cfg.Content = "新的内容"
	state, err := e.SaveConfig(cfg)
	if err != nil || state.RemainingMS != 40000 {
		t.Fatalf("content update: %+v %v", state, err)
	}
	cfg.IntervalMinutes = 2
	state, err = e.SaveConfig(cfg)
	if err != nil || state.RemainingMS != 120000 || state.Started {
		t.Fatalf("interval update: %+v %v", state, err)
	}
	restored := NewEngine(e.path, &recordingNotifier{})
	defer restored.Close()
	state = restored.State()
	if state.Running || state.Config != cfg || state.RemainingMS != 120000 {
		t.Fatalf("restored: %+v", state)
	}
}

func TestSaveFailureDoesNotStartOrMutateConfiguration(t *testing.T) {
	e, _, _ := fixture(t)
	parent := filepath.Join(filepath.Dir(e.path), "file-not-directory")
	if err := ioutil.WriteFile(parent, []byte("existing data"), 0600); err != nil {
		t.Fatal(err)
	}
	e.path = filepath.Join(parent, "config.json")
	original := e.State().Config
	if _, err := e.Start(Config{Content: "新设置", IntervalMinutes: 1}); err == nil {
		t.Fatal("expected failed save")
	}
	if e.State().Running || e.State().Config != original {
		t.Fatal("save failure changed active settings")
	}
}

func TestCorruptConfigurationIsBackedUp(t *testing.T) {
	e, _, _ := fixture(t)
	data := []byte("{broken config}")
	if err := ioutil.WriteFile(e.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	restored := NewEngine(e.path, &recordingNotifier{})
	defer restored.Close()
	files, err := filepath.Glob(e.path + ".invalid-*")
	if err != nil || len(files) != 1 || restored.State().ConfigWarning == "" {
		t.Fatal("missing corrupt config backup or warning")
	}
	backup, err := ioutil.ReadFile(files[0])
	if err != nil || string(backup) != string(data) {
		t.Fatal("backup content changed")
	}
}

func TestInvalidConfigurationRejected(t *testing.T) {
	for _, cfg := range []Config{{"", 1}, {"  ", 1}, {"a", 0}, {"a", -1}, {"a", 10081}, {strings.Repeat("中", 501), 1}, {"a\x00b", 1}} {
		if _, err := cfg.Validate(); err == nil {
			t.Fatalf("accepted invalid config: %+v", cfg)
		}
	}
}

func TestNotificationFailureIsVisibleAndNextCycleContinues(t *testing.T) {
	e, n, now := fixture(t)
	n.err = errors.New("notification unavailable")
	if _, err := e.Start(Config{Content: "休息", IntervalMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	e.checkDue()
	if e.State().NotificationError == "" || !e.State().Running || e.State().NotificationCount != 0 {
		t.Fatal("failure not exposed")
	}
	n.err = nil
	*now = now.Add(time.Minute)
	e.checkDue()
	if e.State().NotificationError != "" || e.State().NotificationCount != 1 {
		t.Fatal("did not recover")
	}
}

func TestTestNotificationDoesNotChangeCountdown(t *testing.T) {
	e, n, _ := fixture(t)
	before := e.State()
	if err := e.TestNotification(Config{Content: "测试", IntervalMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	after := e.State()
	if len(n.messages) != 1 || before.RemainingMS != after.RemainingMS || after.Running || before.Config != after.Config {
		t.Fatal("test notification changed timer")
	}
}

type blockingNotifier struct {
	entered chan struct{}
	exited  chan struct{}
}

func (n *blockingNotifier) Notify(ctx context.Context, title, content string) error {
	close(n.entered)
	<-ctx.Done()
	close(n.exited)
	return ctx.Err()
}

func TestPauseCancelsInFlightNotification(t *testing.T) {
	e, _, now := fixture(t)
	n := &blockingNotifier{entered: make(chan struct{}), exited: make(chan struct{})}
	e.notifier = n
	if _, err := e.Start(Config{Content: "取消", IntervalMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	done := make(chan struct{})
	go func() { e.checkDue(); close(done) }()
	select {
	case <-n.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("notifier did not start")
	}
	e.Pause()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pause did not cancel notifier")
	}
	if e.State().NotificationError != "" || e.State().NotificationCount != 0 {
		t.Fatal("cancel treated as notification failure or success")
	}
}
