package main

import (
	"errors"
	"testing"
)

func TestClaimBesideARunningBuddy(t *testing.T) {
	running, err := claimInstance()
	if err != nil {
		t.Skipf("a real Buddy holds the claim: %v", err)
	}
	defer running()

	if _, _, err := claim(false); !errors.Is(err, errAlreadyRunning) {
		t.Errorf("starting a second Buddy: err = %v, want errAlreadyRunning", err)
	}
	shipped, release, err := claim(true)
	if err != nil {
		t.Fatalf("a report beside a running Buddy: %v", err)
	}
	defer release()
	if shipped != "" {
		t.Errorf("a report beside a running Buddy may install from %q, want nowhere", shipped)
	}
}

func TestClaimAlone(t *testing.T) {
	shipped, release, err := claim(false)
	if err != nil {
		t.Skipf("a real Buddy holds the claim: %v", err)
	}
	defer release()
	if shipped == "" {
		t.Error("the only Buddy has no folder to install from")
	}
}
