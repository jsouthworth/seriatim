package seriatim

type Message interface {
	Purged()
}

type Queue struct {
	queue chan Message
}

func NewQueue(limit int) *Queue {
	q := &Queue{
		queue: make(chan Message, limit),
	}
	return q
}

func (q *Queue) Dequeue() <-chan Message {
	return q.queue
}

func (q *Queue) Enqueue() chan<- Message {
	return q.queue
}

func (q *Queue) Stop() {
	q.drain()
	close(q.queue)
}

func (q *Queue) drain() {
	for {
		select {
		case m := <-q.queue:
			m.Purged()
			continue
		default:
			return
		}
	}
}
