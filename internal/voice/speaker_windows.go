package voice

import (
	"fmt"
	"runtime"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// Speaker reads lines aloud with the Windows speech engine (SAPI). It works
// offline with the voices that come with Windows.
type Speaker struct {
	lines chan string
	done  chan struct{}
}

// NewSpeaker starts the speech engine on its own OS thread, as COM requires.
func NewSpeaker() (*Speaker, error) {
	s := &Speaker{lines: make(chan string, 1), done: make(chan struct{})}
	ready := make(chan error, 1)
	go s.run(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Speaker) run(ready chan<- error) {
	defer close(s.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		ready <- fmt.Errorf("start speech: %w", err)
		return
	}
	defer ole.CoUninitialize()
	unk, err := oleutil.CreateObject("SAPI.SpVoice")
	if err != nil {
		ready <- fmt.Errorf("start speech: %w", err)
		return
	}
	defer unk.Release()
	v, err := unk.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		ready <- fmt.Errorf("start speech: %w", err)
		return
	}
	defer v.Release()
	ready <- nil

	for line := range s.lines {
		// Errors only mean this line isn't heard; the bubble still shows it.
		_, _ = oleutil.CallMethod(v, "Speak", line, 0)
	}
}

// Say queues a line. If Buddy is still talking, the line is dropped rather
// than piling up.
func (s *Speaker) Say(line string) {
	select {
	case s.lines <- line:
	default:
	}
}

// Close stops the speech engine after the current line.
func (s *Speaker) Close() {
	close(s.lines)
	<-s.done
}
