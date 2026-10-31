//go:build !windows

package main

import "errors"

var errAlreadyRunning = errors.New("Buddy is already running")

// claimInstance does nothing outside Windows.
func claimInstance() (func(), error) { return func() {}, nil }

func notify(string) {}
