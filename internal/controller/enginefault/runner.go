// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/llm-d-incubation/llm-d-resiliency-operator/internal/engine"
)

// Runner shares the manager's leadership and cancellation lifecycle.
type Runner struct {
	Adapter  engine.Adapter
	Workload Workload
	Config   Config
}

func (*Runner) NeedLeaderElection() bool { return true }

func (runner *Runner) Start(ctx context.Context) error {
	ticker := time.NewTicker(runner.Config.PollInterval)
	defer ticker.Stop()
	var controller *Controller
	for {
		// Returning would stop the manager and every controller in it. Retry
		// until the journal is readable and valid instead.
		if controller == nil {
			var err error
			if controller, err = New(ctx, runner.Adapter, runner.Workload, runner.Config); err != nil && ctx.Err() == nil {
				ctrl.LoggerFrom(ctx).Error(err, "load engine fault state")
			}
		}
		if controller != nil {
			if err := controller.Step(ctx); err != nil && ctx.Err() == nil {
				ctrl.LoggerFrom(ctx).Error(err, "reconcile engine fault")
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
