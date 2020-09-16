package main

import (
	"errors"
	"sync"
	"time"
)

type Queue struct {
	queue []interface{}
	notify chan int
	lock sync.Mutex
}

func NewQueue() *Queue {
	return &Queue {
		queue: make([]interface{}, 0),
		notify: make(chan int),
	}
}

func (q *Queue) GetLen() int {
	q.lock.Lock()
	defer q.lock.Unlock()
	return len(q.queue)
}

func (q *Queue) GetFirst() interface{} {
	q.lock.Lock()
	defer q.lock.Unlock()
	if len(q.queue) == 0 {
		return nil
	}

	return q.queue[0]
}

func (q *Queue) Enqueue(item interface{}) {
	q.lock.Lock()
	q.queue = append(q.queue, item)
	q.lock.Unlock()

	select {
	case q.notify <- 1:
	default:
	}
}

func (q *Queue) Dequeue() (interface{}, error) {
	q.lock.Lock()
	for len(q.queue) == 0 {
		q.lock.Unlock()

		<-q.notify

		q.lock.Lock()
	}

	item := q.queue[0]
	q.queue = q.queue[1:]

	q.lock.Unlock()
	return item, nil
}

func (q *Queue) DequeueTimeout(timeout time.Duration) (interface{}, error) {
	q.lock.Lock()
	for len(q.queue) == 0 {
		q.lock.Unlock()

		select {
		case <-q.notify:
			break
		case <-time.After(timeout):
			return nil, errors.New("timeout")
		}

		q.lock.Lock()
	}

	item := q.queue[0]
	q.queue = q.queue[1:]

	q.lock.Unlock()
	return item, nil
}