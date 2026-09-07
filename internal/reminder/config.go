package reminder

import (
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

type Config struct {
	Content         string `json:"content"`
	IntervalMinutes int    `json:"intervalMinutes"`
}

func DefaultConfig() Config {
	return Config{Content: "起身走走，喝一杯水。", IntervalMinutes: 30}
}

func (c Config) Validate() (Config, error) {
	c.Content = strings.TrimSpace(c.Content)
	if c.Content == "" {
		return c, errors.New("请输入提醒内容")
	}
	if !utf8.ValidString(c.Content) || utf8.RuneCountInString(c.Content) > 500 {
		return c, errors.New("提醒内容最多 500 个字符")
	}
	for _, r := range c.Content {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			return c, errors.New("提醒内容含不支持的控制字符")
		}
	}
	if c.IntervalMinutes < 1 || c.IntervalMinutes > 10080 {
		return c, errors.New("提醒间隔必须是 1～10080 之间的整数分钟")
	}
	return c, nil
}

func DefaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "DesktopReminder", "config.json")
}

func loadConfig(path string) (Config, error) {
	if path == "" {
		return DefaultConfig(), errors.New("无法定位用户配置目录，暂时不能保存设置")
	}
	data, err := ioutil.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return DefaultConfig(), fmt.Errorf("读取上次配置失败：%w", err)
	}
	var config Config
	err = json.Unmarshal(data, &config)
	if err == nil {
		config, err = config.Validate()
	}
	if err == nil {
		return config, nil
	}
	backup := path + ".invalid-" + time.Now().Format("20060102-150405.000000000")
	if backupErr := ioutil.WriteFile(backup, data, 0600); backupErr != nil {
		return DefaultConfig(), fmt.Errorf("上次配置无法读取，备份失败；已显示默认设置：%w", backupErr)
	}
	return DefaultConfig(), errors.New("上次配置已损坏，原文件已备份；请检查默认设置后开始")
}

// 先完整写入同目录临时文件，再替换配置；保存失败不会破坏旧配置。
func saveConfig(path string, config Config) error {
	if path == "" {
		return errors.New("无法定位用户配置目录")
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := ioutil.TempFile(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
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
	return os.Rename(f.Name(), path)
}
