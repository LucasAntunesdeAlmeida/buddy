//go:build !windows

package ui

import (
	"context"
	"errors"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/brain"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
)

// Run is only implemented on Windows.
func Run(context.Context, <-chan brain.Command, chan<- rl.Reaction, Options) error {
	return errors.New("Buddy's window needs Windows")
}
