package app

import (
	"context"
	"testing"
	"time"
)

func TestBackgroundStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{ctx: ctx}
	stopped := make(chan struct{})
	app.Go(func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // finishing its step
		close(stopped)
	})
	if app.Wait(50 * time.Millisecond) {
		t.Fatal("Wait returned while the work was running")
	}
	cancel()
	if !app.Wait(time.Second) {
		t.Fatal("the work didn't stop with its context")
	}
	select {
	case <-stopped:
	default:
		t.Error("Wait returned before the work finished")
	}
}

func TestGoBeforeStart(t *testing.T) {
	app := &App{}
	ran := make(chan error, 1)
	app.Go(func(ctx context.Context) { ran <- ctx.Err() })
	if err := <-ran; err != nil {
		t.Errorf("context of the work started before Start: %v", err)
	}
	if !app.Wait(time.Second) {
		t.Error("Wait")
	}
}
