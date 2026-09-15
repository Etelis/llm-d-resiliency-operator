// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0
//
//nolint:testpackage // Reuse controller fakes to exercise the manager lifecycle.
package enginefault

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
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

func TestRunnerRevalidatesSavedAdmissionAndStops(t *testing.T) {
	for _, cancelDuringVerify := range []bool{false, true} {
		name := "after verification"
		if cancelDuringVerify {
			name = "during verification"
		}
		t.Run(name, func(t *testing.T) {
			_, adapter, workload := setup(t)
			held := &heldVerification{fakeAdapter: adapter, entered: make(chan struct{}), release: make(chan struct{})}
			config := Defaults()
			config.PollInterval = time.Hour
			runner := &Runner{Adapter: held, Workload: workload, Config: config, Address: unusedAddress(t)}
			if !runner.NeedLeaderElection() || runner.Ready(nil) == nil {
				t.Fatal("runner must require leadership and start unready")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- runner.Start(ctx) }()
			select {
			case <-held.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("runner did not verify restored state")
			}
			if err := runner.Ready(nil); err != nil {
				t.Fatalf("active admission server is not ready: %v", err)
			}
			assertAdmission(t, runner.Address, false)
			if !cancelDuringVerify {
				close(held.release)
				assertAdmission(t, runner.Address, true)
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
			if runner.Ready(nil) == nil {
				t.Fatal("stopped runner remains ready")
			}
			if len(workload.resets) != 0 {
				t.Fatal("shutdown triggered a workload reset")
			}
			client := &http.Client{Timeout: time.Second}
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
				"http://"+runner.Address+"/admission?rank=0&pod_uid=a", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if err == nil {
				response.Body.Close()
				t.Fatal("stopped runner still accepts admission requests")
			}
		})
	}
}

func TestRunnerStartupFailureRemainsUnready(t *testing.T) {
	for _, invalidState := range []bool{false, true} {
		name := "occupied address"
		if invalidState {
			name = "invalid saved state"
		}
		t.Run(name, func(t *testing.T) {
			_, adapter, workload := setup(t)
			listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if invalidState {
				workload.saved = []byte("invalid")
			}
			runner := &Runner{Adapter: adapter, Workload: workload, Config: Defaults(), Address: listener.Addr().String()}
			if err := runner.Start(context.Background()); err == nil || runner.Ready(nil) == nil {
				t.Fatal("failed startup reported ready or returned success")
			}
		})
	}
}

func unusedAddress(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func assertAdmission(t *testing.T, address string, want bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet,
			"http://"+address+"/admission?rank=0&pod_uid=a", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var result struct{ Admitted bool }
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if result.Admitted == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("admission did not become %v", want)
		case <-ticker.C:
		}
	}
}
