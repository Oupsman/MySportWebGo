package activityService

import (
	"errors"
	"testing"
	"time"
)

func TestSlots(t *testing.T) {
	s := newSlots(2)
	first, err := s.acquire(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.acquire(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// both taken: the third one gives up
	if _, err := s.acquire(10 * time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("third job: %v, want ErrBusy", err)
	}
	// a slot freed while waiting is taken
	go func() {
		time.Sleep(10 * time.Millisecond)
		first()
	}()
	if _, err := s.acquire(time.Second); err != nil {
		t.Fatalf("after a release: %v", err)
	}
}
