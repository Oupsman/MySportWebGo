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

func TestAnalysesFor(t *testing.T) {
	for limit, want := range map[int64]int{0: 4, 256 << 20: 1, 512 << 20: 1, 1 << 30: 2, 1536 << 20: 3, 2 << 30: 4, 32 << 30: 4} {
		if got := AnalysesFor(limit); got != want {
			t.Errorf("%d MB: %d analyses, want %d", limit>>20, got, want)
		}
	}
}

func TestSetConcurrentAnalyses(t *testing.T) {
	saved := MaxConcurrentAnalyses
	t.Cleanup(func() { SetConcurrentAnalyses(saved) })
	SetConcurrentAnalyses(2)
	if MaxConcurrentAnalyses != 2 || cap(analysisSlots) != 2 {
		t.Errorf("%d analyses, %d slots", MaxConcurrentAnalyses, cap(analysisSlots))
	}
}
