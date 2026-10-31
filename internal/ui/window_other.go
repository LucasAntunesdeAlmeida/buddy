//go:build !windows

package ui

import (
	"context"
	"errors"

	"buddy/internal/brain"
	"buddy/internal/rl"
)

// Run is only implemented on Windows.
func Run(context.Context, <-chan brain.Command, chan<- rl.Reaction) error {
	return errors.New("Buddy's window needs Windows")
}
