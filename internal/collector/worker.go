package collector

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RunOutcome struct {
	RunID      string
	Checkpoint []byte
	Complete   bool
}

type Handler func(context.Context, Claim) (RunOutcome, error)

type Worker struct {
	Store   Store
	Owner   string
	Lease   time.Duration
	Handler Handler
	Now     func() time.Time
}

func (w Worker) RunOnce(ctx context.Context) error {
	if w.Handler == nil {
		return errors.New("collector handler is required")
	}
	lease := w.Lease
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	claim, err := w.Store.ClaimDue(ctx, w.Owner, lease)
	if errors.Is(err, ErrNoDueSchedule) {
		return nil
	}
	if err != nil {
		return err
	}
	outcome, err := w.Handler(ctx, claim)
	if err != nil {
		retryAt := w.now().Add(time.Duration(claim.IntervalSeconds) * time.Second)
		if failErr := w.Store.Fail(ctx, claim, "handler_failed", retryAt); failErr != nil {
			return fmt.Errorf("collector handler failed: %v; fail schedule: %w", err, failErr)
		}
		return err
	}
	if !outcome.Complete {
		return errors.New("collector handler returned incomplete outcome")
	}
	return w.Store.Complete(ctx, claim, outcome.RunID, outcome.Checkpoint, w.now().Add(time.Duration(claim.IntervalSeconds)*time.Second))
}

func (w Worker) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = time.Second
	}
	_ = w.RunOnce(ctx)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil && errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
		}
	}
}

func (w Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
