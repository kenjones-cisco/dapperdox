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
	add := func(obj interface{}, event models.Event) error {
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
	q.Push(Task{handler: func(obj interface{}, event models.Event) error {
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
		return func(obj interface{}, event models.Event) error {
			out += i

			return nil
		}
	}

	handler := ChainHandler{
		funcs: []Handler{f(1), f(2)},
	}

	go q.Run(stop)

	q.Push(Task{handler: handler.Apply, obj: 0})
	q.Push(Task{handler: func(obj interface{}, event models.Event) error {
		if out != 3 {
			t.Errorf("ChainedHandler => %d, want %d", out, 3)
		}

		close(stop)

		return nil
	}, obj: 0})
}
