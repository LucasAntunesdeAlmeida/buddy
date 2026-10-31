//go:build !windows

package voice

import "errors"

// Speaker is not available outside Windows.
type Speaker struct{}

// NewSpeaker always fails outside Windows.
func NewSpeaker() (*Speaker, error) { return nil, errors.New("speech needs Windows") }

// Say does nothing.
func (s *Speaker) Say(string) {}

// Close does nothing.
func (s *Speaker) Close() {}
