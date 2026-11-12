package discover

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kenjones-cisco/dapperdox/discover/models"
)

var errTestRetry = errors.New("intentional retry")

func TestQueue(t *testing.T) {
	_ = NewTask(nil, nil, models.EventAdd)
	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})

	var stopped sync.Once

	stopQueue := func() { stopped.Do(func() { close(stop) }) }
	defer stopQueue()

	out := 0
	err := true
	add := func(obj any, _ models.Event) error {
		log().Info(fmt.Sprintf("adding %d, error: %t", obj.(int), err))
		objCnt, _ := obj.(int)

		out += objCnt

		if !err {
			return nil
		}

		err = false

		return errTestRetry
	}

	done := make(chan struct{})

	go func() {
		q.Run(stop)
		close(done)
	}()

	q.Push(Task{handler: add, obj: 1})
	q.Push(Task{handler: add, obj: 2})
	q.Push(Task{handler: func(_ any, _ models.Event) error {
		if out != 4 {
			t.Errorf("Queue => %d, want %d", out, 4)
		}

		stopQueue()

		return nil
	}, obj: 0})

	select {
	case <-done:
	case <-time.After(time.Second):
		stopQueue()
		t.Fatal("queue did not process tasks")
	}
}

func TestChainedHandler(t *testing.T) {
	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})

	var stopped sync.Once

	stopQueue := func() { stopped.Do(func() { close(stop) }) }
	defer stopQueue()

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

	done := make(chan struct{})

	go func() {
		q.Run(stop)
		close(done)
	}()

	q.Push(Task{handler: handler.Apply, obj: 0})
	q.Push(Task{handler: func(_ any, _ models.Event) error {
		if out != 3 {
			t.Errorf("ChainedHandler => %d, want %d", out, 3)
		}

		stopQueue()

		return nil
	}, obj: 0})

	select {
	case <-done:
	case <-time.After(time.Second):
		stopQueue()
		t.Fatal("queue did not process chained handlers")
	}
}

func TestQueuePreservesFIFOAndDuplicateEventsThroughRetries(t *testing.T) {
	queue := NewQueue(time.Millisecond)
	stop := make(chan struct{})
	done := make(chan struct{})
	processed := make(chan models.Event, 4)
	attempts := 0
	handler := func(_ any, event models.Event) error {
		processed <- event

		attempts++
		if attempts == 1 {
			return errTestRetry
		}

		return nil
	}

	for _, event := range []models.Event{models.EventAdd, models.EventAdd, models.EventDelete} {
		queue.Push(NewTask(handler, "same-object", event))
	}

	go func() {
		queue.Run(stop)
		close(done)
	}()

	defer func() {
		close(stop)
		<-done
	}()

	for _, want := range []models.Event{models.EventAdd, models.EventAdd, models.EventAdd, models.EventDelete} {
		select {
		case got := <-processed:
			if got != want {
				t.Fatalf("event = %v, want %v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("queue lost an event")
		}
	}
}

func TestQueueRetriesEntireHandlerChainBeforeLaterTasks(t *testing.T) {
	queue := NewQueue(time.Millisecond)
	stop := make(chan struct{})
	done := make(chan struct{})
	processed := make(chan string, 6)
	attempts := 0
	chain := &ChainHandler{}
	chain.Append(func(any, models.Event) error {
		processed <- "first"

		return nil
	})
	chain.Append(func(any, models.Event) error {
		processed <- "second"

		attempts++
		if attempts == 1 {
			return errTestRetry
		}

		return nil
	})
	chain.Append(func(any, models.Event) error {
		processed <- "third"

		return nil
	})
	queue.Push(NewTask(chain.Apply, nil, models.EventAdd))
	queue.Push(NewTask(func(any, models.Event) error {
		processed <- "next task"

		return nil
	}, nil, models.EventAdd))

	go func() {
		queue.Run(stop)
		close(done)
	}()

	defer func() {
		close(stop)
		<-done
	}()

	for _, want := range []string{"first", "second", "first", "second", "third", "next task"} {
		select {
		case got := <-processed:
			if got != want {
				t.Fatalf("handler = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("queue did not finish retrying handler chain")
		}
	}
}

func TestQueueAcceptsConcurrentProducers(t *testing.T) {
	queue := NewQueue(time.Millisecond)
	stop := make(chan struct{})
	done := make(chan struct{})
	processed := make(chan int, 64)
	handler := Handler(func(obj any, _ models.Event) error {
		value, _ := obj.(int)
		processed <- value

		return nil
	})

	go func() {
		queue.Run(stop)
		close(done)
	}()

	defer func() {
		close(stop)
		<-done
	}()

	var producers sync.WaitGroup
	for value := range 64 {
		producers.Go(func() { queue.Push(NewTask(handler, value, models.EventAdd)) })
	}

	producers.Wait()

	got := make([]int, 0, 64)

	for range 64 {
		select {
		case value := <-processed:
			got = append(got, value)
		case <-time.After(time.Second):
			t.Fatal("queue lost concurrent work")
		}
	}

	slices.Sort(got)

	for value := range 64 {
		if got[value] != value {
			t.Fatalf("task %d processed incorrectly: %v", value, got)
		}
	}
}

func TestQueueStopsWithoutProcessingPendingOrFutureTasks(t *testing.T) {
	queue := NewQueue(time.Millisecond)
	stop := make(chan struct{})
	close(stop)

	processed := make(chan struct{}, 2)
	task := NewTask(func(any, models.Event) error {
		processed <- struct{}{}

		return nil
	}, nil, models.EventAdd)
	queue.Push(task)
	queue.Run(stop)
	queue.Push(task)
	queue.Run(stop)

	select {
	case <-processed:
		t.Fatal("stopped queue processed work")
	default:
	}
}

func TestQueueWakesAfterBecomingIdle(t *testing.T) {
	queue := NewQueue(time.Millisecond)
	stop := make(chan struct{})
	done := make(chan struct{})
	processed := make(chan struct{}, 1)

	go func() {
		queue.Run(stop)
		close(done)
	}()

	defer func() {
		close(stop)
		<-done
	}()

	for range 5 {
		time.Sleep(20 * time.Millisecond)
		queue.Push(NewTask(func(any, models.Event) error {
			processed <- struct{}{}

			return nil
		}, nil, models.EventAdd))

		select {
		case <-processed:
		case <-time.After(time.Second):
			t.Fatal("idle queue did not wake for new work")
		}
	}
}

func TestQueuePreservesProcessingRate(t *testing.T) {
	queue := NewQueue(time.Millisecond)
	stop := make(chan struct{})
	done := make(chan struct{})
	processed := make(chan time.Time, 103)

	for range 103 {
		queue.Push(NewTask(func(any, models.Event) error {
			processed <- time.Now()

			return nil
		}, nil, models.EventAdd))
	}

	start := time.Now()

	go func() {
		queue.Run(stop)
		close(done)
	}()

	defer func() {
		close(stop)
		<-done
	}()

	var last time.Time

	for range 103 {
		select {
		case last = <-processed:
		case <-time.After(2 * time.Second):
			t.Fatal("rate-limited queue stalled")
		}
	}

	if elapsed := last.Sub(start); elapsed < 290*time.Millisecond {
		t.Fatalf("103 tasks exceeded the 100-task burst and 10/sec rate: %v", elapsed)
	}
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

		return errTestRetry
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
