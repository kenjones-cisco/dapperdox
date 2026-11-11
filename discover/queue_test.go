package discover

import (
	"errors"
	"testing"
	"time"

	"github.com/kenjones-cisco/dapperdox/discover/models"
)

func TestQueue(t *testing.T) {
	_ = NewTask(nil, nil, models.EventAdd)
	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})
	out := 0
	err := true
	add := func(obj any, _ models.Event) error {
		log().Infof("adding %d, error: %t", obj.(int), err)
		objCnt, _ := obj.(int)

		out += objCnt

		if !err {
			return nil
		}

		err = false

		return errors.New("intentional error")
	}

	go q.Run(stop)

	q.Push(Task{handler: add, obj: 1})
	q.Push(Task{handler: add, obj: 2})
	q.Push(Task{handler: func(_ any, _ models.Event) error {
		if out != 4 {
			t.Errorf("Queue => %d, want %d", out, 4)
		}

		close(stop)

		return nil
	}, obj: 0})
}

func TestChainedHandler(t *testing.T) {
	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})
	out := 0
	f := func(i int) Handler {
		return func(_ any, _ models.Event) error {
			out += i

			return nil
		}
	}

	handler := ChainHandler{
		funcs: []Handler{f(1), f(2)},
	}

	go q.Run(stop)

	q.Push(Task{handler: handler.Apply, obj: 0})
	q.Push(Task{handler: func(_ any, _ models.Event) error {
		if out != 3 {
			t.Errorf("ChainedHandler => %d, want %d", out, 3)
		}

		close(stop)

		return nil
	}, obj: 0})
}

func TestQueueShutdownCancelsRetryDelay(t *testing.T) {
	q := NewQueue(time.Hour)
	stop := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{})

	go func() {
		q.Run(stop)
		close(done)
	}()
	q.Push(Task{handler: func(any, models.Event) error {
		close(started)

		return errors.New("retry")
	}})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queue handler did not start")
	}
	close(stop)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queue did not stop during retry delay")
	}
}

func TestQueueIdleTracksQueuedAndActiveWork(t *testing.T) {
	queue := NewQueue(time.Hour).(*queueImpl)
	if !queue.Idle() {
		t.Fatal("new queue should be idle")
	}

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	stop := make(chan struct{})

	go func() {
		queue.Run(stop)
		close(done)
	}()
	queue.Push(Task{handler: func(any, models.Event) error {
		close(started)
		<-release

		return nil
	}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queue handler did not start")
	}

	if queue.Idle() {
		t.Fatal("queue reported idle while processing work")
	}

	close(release)

	deadline := time.After(time.Second)

	for !queue.Idle() {
		select {
		case <-time.After(time.Millisecond):
		case <-deadline:
			t.Fatal("queue did not return to idle")
		}
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queue did not stop")
	}
}
