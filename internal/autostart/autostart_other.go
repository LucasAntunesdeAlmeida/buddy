//go:build !windows

// Package autostart starts Buddy when you sign in. Only Windows is supported.
package autostart

import "errors"

// Entry does nothing outside Windows.
type Entry struct{}

// New returns a stand-in.
func New() *Entry { return &Entry{} }

// Available is always false.
func (e *Entry) Available() bool { return false }

// Enabled is always false.
func (e *Entry) Enabled() bool { return false }

// Set always fails.
func (e *Entry) Set(bool) error { return errors.New("start with Windows needs Windows") }
