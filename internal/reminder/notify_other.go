//go:build !windows
// +build !windows

package reminder

import (
	"context"
	"errors"
)

type unsupportedNotifier struct{}

func NewNotifier() Notifier { return &unsupportedNotifier{} }
func (n *unsupportedNotifier) Notify(context.Context, string, string) error {
	return errors.New("桌面通知仅支持 Windows 10 / 11")
}
