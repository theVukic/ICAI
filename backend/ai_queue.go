package main

import "sync"

type aiCatalogJob struct {
	ItemID     string
	Generation uint64
	Kind       string
}

// aiJobQueue is an in-memory unbounded FIFO. Catalogs with thousands of rows
// must not fail merely because a fixed channel buffer is full.
type aiJobQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	jobs   []aiCatalogJob
	queued map[string]uint64
	closed bool
}

func newAIJobQueue() *aiJobQueue {
	queue := &aiJobQueue{queued: map[string]uint64{}}
	queue.cond = sync.NewCond(&queue.mu)
	return queue
}

func (q *aiJobQueue) Push(job aiCatalogJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || job.ItemID == "" {
		return
	}
	key := job.ItemID + ":" + job.Kind
	if generation, exists := q.queued[key]; exists && generation == job.Generation {
		return
	}
	q.jobs = append(q.jobs, job)
	q.queued[key] = job.Generation
	q.cond.Signal()
}

func (q *aiJobQueue) Pop() (aiCatalogJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.jobs) == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.closed {
		return aiCatalogJob{}, false
	}
	job := q.jobs[0]
	q.jobs[0] = aiCatalogJob{}
	q.jobs = q.jobs[1:]
	delete(q.queued, job.ItemID+":"+job.Kind)
	return job, true
}

func (q *aiJobQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = nil
	q.queued = map[string]uint64{}
}

func (q *aiJobQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.jobs = nil
	q.queued = map[string]uint64{}
	q.cond.Broadcast()
}
