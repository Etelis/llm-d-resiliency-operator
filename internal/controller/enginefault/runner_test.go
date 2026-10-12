// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0
//
//nolint:testpackage // Reuse controller fakes to exercise manager cancellation.
package enginefault

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/llm-d-incubation/llm-d-resiliency-operator/internal/engine"
)

type heldVerification struct {
	*fakeAdapter
	entered chan struct{}
	release chan struct{}
}

func (adapter *heldVerification) Verify(ctx context.Context, group engine.Group, ranks []int) error {
	close(adapter.entered)
	select {
	case <-adapter.release:
		return adapter.fakeAdapter.Verify(ctx, group, ranks)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRunnerRevalidatesSavedStateAndCancelsVerification(t *testing.T) {
	_, adapter, workload := setup(t)
	held := &heldVerification{fakeAdapter: adapter, entered: make(chan struct{}), release: make(chan struct{})}
	config := Defaults()
	config.PollInterval = time.Hour
	runner := &Runner{Adapter: held, Workload: workload, Config: config}
	if !runner.NeedLeaderElection() {
		t.Fatal("runner must require leadership")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- runner.Start(ctx) }()
	select {
	case <-held.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not reverify saved state")
	}
	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop on cancellation")
	}
	if len(workload.resets) != 0 {
		t.Fatal("shutdown triggered reset")
	}
}

func TestRunnerWaitsOnInvalidSavedState(t *testing.T) {
	_, adapter, workload := setup(t)
	workload.saved = []byte("invalid")
	verifications := adapter.verifications
	config := Defaults()
	config.PollInterval = 10 * time.Millisecond
	runner := &Runner{Adapter: adapter, Workload: workload, Config: config}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// Invalid state is never used, and returning would stop the manager and
	// every other controller in it.
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if adapter.verifications != verifications || len(adapter.dispatches) != 0 || len(workload.resets) != 0 ||
		string(workload.saved) != "invalid" {
		t.Fatal("runner acted on invalid saved state")
	}
}

type unreadableJournal struct {
	*fakeWorkload
	failures atomic.Int32
}

func (workload *unreadableJournal) Load(ctx context.Context) ([]byte, error) {
	if workload.failures.Add(-1) >= 0 {
		return nil, errors.New("Kubernetes API unavailable")
	}
	return workload.fakeWorkload.Load(ctx)
}

func TestRunnerRetriesUnreadableJournal(t *testing.T) {
	_, adapter, workload := setup(t)
	journal := &unreadableJournal{fakeWorkload: workload}
	journal.failures.Store(3)
	held := &heldVerification{fakeAdapter: adapter, entered: make(chan struct{}), release: make(chan struct{})}
	config := Defaults()
	config.PollInterval = 10 * time.Millisecond
	runner := &Runner{Adapter: held, Workload: journal, Config: config}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- runner.Start(ctx) }()
	select {
	case <-held.entered:
	case err := <-stopped:
		t.Fatalf("runner stopped instead of retrying the journal: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not resume once the journal was readable")
	}
	cancel()
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
}
