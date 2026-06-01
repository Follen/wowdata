package prepare

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrContextNotReady = errors.New("context_not_ready")
	ErrBuildNotReady   = errors.New("build_not_ready")
	ErrTableNotReady   = errors.New("table_not_ready")
)

type Target struct {
	Label   string
	Region  string
	Product string
	Locale  string
	Tables  []string
}

type Limits struct {
	MaxParallelContextPrepares       int
	MaxParallelTableMaterializations int
}

type ContextPreparer func(context.Context, Target) error

type TableMaterializer func(context.Context, Target, string) error

type TargetState struct {
	ContextReady bool
	TablesReady  bool
	State        string
	Error        string
}

type Scheduler struct {
	targets     []Target
	limits      Limits
	prepare     ContextPreparer
	materialize TableMaterializer

	contextSem chan struct{}
	tableSem   chan struct{}

	mu     sync.RWMutex
	states map[string]TargetState

	startOnce sync.Once
	wg        sync.WaitGroup

	errMu sync.Mutex
	errs  []error
}

func NewScheduler(targets []Target, limits Limits, prepare ContextPreparer, materialize TableMaterializer) *Scheduler {
	if limits.MaxParallelContextPrepares <= 0 {
		limits.MaxParallelContextPrepares = 1
	}
	if limits.MaxParallelTableMaterializations <= 0 {
		limits.MaxParallelTableMaterializations = 1
	}

	copiedTargets := append([]Target(nil), targets...)
	states := make(map[string]TargetState, len(copiedTargets))
	for _, target := range copiedTargets {
		states[targetKey(target)] = TargetState{State: "queued"}
	}

	return &Scheduler{
		targets:     copiedTargets,
		limits:      limits,
		prepare:     prepare,
		materialize: materialize,
		contextSem:  make(chan struct{}, limits.MaxParallelContextPrepares),
		tableSem:    make(chan struct{}, limits.MaxParallelTableMaterializations),
		states:      states,
	}
}

func (s *Scheduler) StartAfterReady(ctx context.Context, listenerReady <-chan struct{}) {
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			select {
			case <-listenerReady:
			case <-ctx.Done():
				s.addError(ctx.Err())
				return
			}

			for _, target := range s.targets {
				target := target
				s.wg.Add(1)
				go func() {
					defer s.wg.Done()
					s.prepareTarget(ctx, target)
				}()
			}
		}()
	})
}

func (s *Scheduler) Wait() error {
	s.wg.Wait()

	s.errMu.Lock()
	defer s.errMu.Unlock()
	if len(s.errs) == 0 {
		return nil
	}
	return errors.Join(s.errs...)
}

func (s *Scheduler) CheckReadyForUserQuery(target Target) error {
	s.mu.RLock()
	state, ok := s.states[targetKey(target)]
	s.mu.RUnlock()
	if !ok || !state.ContextReady {
		return ErrContextNotReady
	}
	if !state.TablesReady {
		if len(target.Tables) > 0 {
			return ErrTableNotReady
		}
		return ErrBuildNotReady
	}
	return nil
}

func (s *Scheduler) State(target Target) TargetState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.states[targetKey(target)]
}

func (s *Scheduler) prepareTarget(ctx context.Context, target Target) {
	if s.prepare == nil {
		err := errors.New("context preparer is required")
		s.markFailed(target, err)
		s.addError(err)
		return
	}
	s.setState(target, TargetState{State: "preparing_context"})
	if err := s.withSemaphore(ctx, s.contextSem, func() error {
		return s.prepare(ctx, target)
	}); err != nil {
		s.markFailed(target, err)
		s.addError(fmt.Errorf("prepare %s: %w", targetKey(target), err))
		return
	}

	s.setState(target, TargetState{State: "materializing_tables", ContextReady: true})
	if err := s.materializeTables(ctx, target); err != nil {
		s.markFailed(target, err)
		s.addError(fmt.Errorf("materialize %s: %w", targetKey(target), err))
		return
	}
	s.setState(target, TargetState{State: "ready", ContextReady: true, TablesReady: true})
}

func (s *Scheduler) materializeTables(ctx context.Context, target Target) error {
	if len(target.Tables) == 0 {
		return nil
	}
	if s.materialize == nil {
		return errors.New("table materializer is required")
	}

	var wg sync.WaitGroup
	var errMu sync.Mutex
	var errs []error
	for _, table := range target.Tables {
		table := table
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.withSemaphore(ctx, s.tableSem, func() error {
				return s.materialize(ctx, target, table)
			})
			if err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", table, err))
				errMu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (s *Scheduler) withSemaphore(ctx context.Context, sem chan struct{}, run func() error) error {
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-sem }()
	return run()
}

func (s *Scheduler) setState(target Target, state TargetState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[targetKey(target)] = state
}

func (s *Scheduler) markFailed(target Target, err error) {
	s.setState(target, TargetState{State: "failed", Error: err.Error()})
}

func (s *Scheduler) addError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	s.errs = append(s.errs, err)
}

func targetKey(target Target) string {
	parts := []string{target.Label, target.Region, target.Product, target.Locale}
	return strings.Join(parts, "\x00")
}
