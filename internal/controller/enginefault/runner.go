// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"context"
	"errors"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
	"github.com/llm-d/llm-d-resiliency-operator/internal/journal"
	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

// Runner shares the manager's leader election and stops with it.
type Runner struct {
	Workload workload.Workload
	Engine   engine.Engine
	Journal  journal.Journal
	Recorder Recorder
	Config   Config
}

func (*Runner) NeedLeaderElection() bool { return true }

func (runner *Runner) Start(ctx context.Context) error {
	ticker := time.NewTicker(runner.Config.PollInterval)
	defer ticker.Stop()
	store := runner.Journal
	if runner.Config.DryRun {
		store = &memJournal{}
	}
	var current Controller
	for {
		// Returning an error would stop the manager and every controller in it.
		if current == nil {
			loaded, err := New(ctx, runner.Workload, runner.Engine, store, runner.Recorder, runner.Config)
			if err != nil && ctx.Err() == nil {
				ctrl.LoggerFrom(ctx).Error(err, "load engine fault journal")
			}
			current = loaded
		}
		if current != nil {
			stepCtx, cancel := context.WithTimeout(ctx, runner.Config.ApplyTimeout)
			if err := current.Step(stepCtx); errors.Is(err, journal.ErrConflict) {
				current = nil
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// memJournal keeps a dry run's record in memory, so it never writes the real journal.
type memJournal struct{ data []byte }

//nolint:gocritic // The contract returns the record, its version and an error.
func (store *memJournal) Load(context.Context) ([]byte, string, error) { return store.data, "", nil }

func (store *memJournal) Save(_ context.Context, data []byte, _ string) (string, error) {
	store.data = data
	return "", nil
}
