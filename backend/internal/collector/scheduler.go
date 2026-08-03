package collector

import (
	"context"
	"log"
	"sync"
	"time"

	"github-cmdb/internal/model"
	"github-cmdb/internal/repository"
)

// Scheduler periodically runs discovery strategies based on their schedule expressions.
type Scheduler struct {
	executor *DiscoveryExecutor
	repo     *repository.DiscoveryRepo
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewScheduler creates a scheduler.
func NewScheduler(executor *DiscoveryExecutor, repo *repository.DiscoveryRepo) *Scheduler {
	return &Scheduler{
		executor: executor,
		repo:     repo,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the scheduling loop in a background goroutine.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go s.loop()
	log.Println("discovery scheduler started")
}

// Stop gracefully shuts down the scheduler.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
	log.Println("discovery scheduler stopped")
}

func (s *Scheduler) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	strategies, err := s.repo.ListEnabled()
	if err != nil {
		log.Printf("scheduler: failed to list strategies: %v", err)
		return
	}

	for _, st := range strategies {
		if !shouldRun(&st) {
			continue
		}
		log.Printf("scheduler: running strategy %q (id=%d)", st.Name, st.ID)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(st.TimeoutSec)*time.Second)
		s.executor.RunStrategy(ctx, &st)
		cancel()
	}
}

// shouldRun checks if enough time has passed since the last run.
func shouldRun(st *model.DiscoveryStrategy) bool {
	if st.ScheduleExpr == nil || *st.ScheduleExpr == "" {
		return false
	}
	if st.LastRunAt == nil {
		return true
	}
	interval, err := parseSimpleInterval(*st.ScheduleExpr)
	if err != nil {
		return false
	}
	return time.Since(*st.LastRunAt) >= interval
}

// parseSimpleInterval handles expressions like "1h", "30m", "6h".
func parseSimpleInterval(expr string) (time.Duration, error) {
	return time.ParseDuration(expr)
}