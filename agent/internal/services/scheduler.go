package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type ScheduledJob func(context.Context)

type Scheduler struct {
	mu   sync.Mutex
	jobs []job
}

type job struct {
	name     string
	interval time.Duration
	fn       ScheduledJob
}

func NewScheduler() *Scheduler {
	return &Scheduler{jobs: make([]job, 0)}
}

func (s *Scheduler) AddTask(name string, interval time.Duration, fn ScheduledJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job{name: name, interval: interval, fn: fn})
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.mu.Lock()
	jobs := make([]job, len(s.jobs))
	copy(jobs, s.jobs)
	s.mu.Unlock()

	if len(jobs) == 0 {
		return fmt.Errorf("нет задач для планировщика")
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()

			ticker := time.NewTicker(j.interval)
			defer ticker.Stop()

			log.Printf("Планировщик: запущена задача %s (интервал %s)", j.name, j.interval)
			j.fn(ctx)

			for {
				select {
				case <-ctx.Done():
					log.Printf("Планировщик: остановка задачи %s", j.name)
					return
				case <-ticker.C:
					j.fn(ctx)
				}
			}
		}(j)
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}
