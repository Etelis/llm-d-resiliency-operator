// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
)

// Runner shares the operator's leadership and shutdown lifecycle.
type Runner struct {
	Adapter  engine.Adapter
	Workload Workload
	Config   Config
	Address  string
	ready    atomic.Bool
}

func (*Runner) NeedLeaderElection() bool { return true }

func (runner *Runner) Ready(*http.Request) error {
	if !runner.ready.Load() {
		return fmt.Errorf("vLLM FT leader is not ready")
	}
	return nil
}

func (runner *Runner) Start(ctx context.Context) error {
	controller, err := New(ctx, runner.Adapter, runner.Workload, runner.Config, nil)
	if err != nil {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", runner.Address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: controller.Handler(), ReadHeaderTimeout: 5 * time.Second}
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	runner.ready.Store(true)
	defer func() {
		runner.ready.Store(false)
		state := controller.Snapshot()
		state.Admitted = nil
		controller.set(state)
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "close FT admission server")
		}
	}()
	ticker := time.NewTicker(runner.Config.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := controller.Step(ctx); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "reconcile engine fault")
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-serveError:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ticker.C:
		}
	}
}
