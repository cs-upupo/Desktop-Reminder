//go:build !windows
// +build !windows

package main

type trayController struct{}

func newTrayController(show, settings, exit func()) *trayController { return &trayController{} }
func (t *trayController) start()                                    {}
func (t *trayController) stop()                                     {}
func (t *trayController) isReady() bool                             { return false }
