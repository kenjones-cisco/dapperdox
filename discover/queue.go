package discover

import (
	"log/slog"
	"sync"
	"time"

	"k8s.io/client-go/util/flowcontrol"

	"github.com/kenjones-cisco/dapperdox/discover/models"
)

const (
	qpsRate  float32 = 10
	qpsBurst int     = 100
)

// Queue of work tickets processed using a rate-limiting loop.
type Queue interface {
	// Push a ticket
	Push(item Task)
	// Run the loop until a signal on the channel
	Run(stop <-chan struct{})
}

// Handler specifies a function to apply on an object for a given event type.
type Handler func(obj any, event models.Event) error

// Task object for the event watchers; processes until handler succeeds.
type Task struct {
	handler Handler
	obj     any
	event   models.Event
}

// NewTask creates a task from a work item.
func NewTask(handler Handler, obj any, event models.Event) Task {
	return Task{handler: handler, obj: obj, event: event}
}

type queueImpl struct {
	queue   []Task
	ready   chan struct{}
	delay   time.Duration
	lock    sync.Mutex
	active  bool
	closing bool
}

// NewQueue instantiates a queue with a processing function.
func NewQueue(errorDelay time.Duration) Queue {
	return &queueImpl{
		delay: errorDelay,
		ready: make(chan struct{}, 1),
	}
}

func (q *queueImpl) Push(item Task) {
	q.lock.Lock()

	if !q.closing {
		q.queue = append(q.queue, item)
		// Only wake-ups coalesce; every task remains in the FIFO.
		select {
		case q.ready <- struct{}{}:
		default:
		}
	}

	q.lock.Unlock()
}

func (q *queueImpl) Run(stop <-chan struct{}) {
	// Throttle processing up to smoothed 10 qps with bursts up to 100 qps
	rateLimiter := flowcontrol.NewTokenBucketRateLimiter(qpsRate, qpsBurst)
	defer rateLimiter.Stop()
	defer q.finish()

	for {
		item, ok := q.next(stop)
		if !ok {
			return
		}

		rateLimiter.Accept()

		select {
		case <-stop:
			return
		default:
		}

		if !q.runTask(item, stop) {
			return
		}

		q.lock.Lock()
		q.active = false
		q.lock.Unlock()
	}
}

func (q *queueImpl) Idle() bool {
	q.lock.Lock()
	defer q.lock.Unlock()

	return len(q.queue) == 0 && !q.active
}

func (q *queueImpl) next(stop <-chan struct{}) (Task, bool) {
	for {
		select {
		case <-stop:
			return Task{}, false
		default:
		}

		q.lock.Lock()
		if q.closing {
			q.lock.Unlock()

			return Task{}, false
		}

		if len(q.queue) > 0 {
			item := q.queue[0]
			q.queue[0] = Task{}

			q.queue = q.queue[1:]
			if len(q.queue) == 0 {
				q.queue = nil
			}

			q.active = true
			q.lock.Unlock()

			return item, true
		}
		q.lock.Unlock()

		select {
		case <-stop:
			return Task{}, false
		case <-q.ready:
		}
	}
}

func (q *queueImpl) finish() {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.active = false
	q.closing = true
	q.queue = nil
}

func (q *queueImpl) runTask(item Task, stop <-chan struct{}) bool {
	for {
		if err := item.handler(item.obj, item.event); err != nil {
			log().Error("work item failed; retrying", slog.Any("error", err), slog.Duration("delay", q.delay))
		} else {
			return true
		}

		timer := time.NewTimer(q.delay)
		select {
		case <-stop:
			timer.Stop()

			return false
		case <-timer.C:
		}
	}
}

// ChainHandler applies handlers in a sequence.
type ChainHandler struct {
	funcs []Handler
}

// Apply is the handler function.
func (ch *ChainHandler) Apply(obj any, event models.Event) error {
	for _, f := range ch.funcs {
		if err := f(obj, event); err != nil {
			return err
		}
	}

	return nil
}

// Append a handler as the last handler in the chain.
func (ch *ChainHandler) Append(h Handler) {
	ch.funcs = append(ch.funcs, h)
}
