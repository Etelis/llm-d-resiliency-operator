// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0
//
//nolint:testpackage // Inspect persisted phase transitions and inject precise deadlines.
package enginefault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
)

func statuses(states ...string) []engine.RankStatus {
	r := make([]engine.RankStatus, 0, len(states))
	for i, s := range states {
		r = append(r, engine.RankStatus{Rank: i, Status: s})
	}
	return r
}

func TestFaultDecisionsPreserveOriginalMembership(t *testing.T) {
	tests := []struct {
		name         string
		states       []engine.RankStatus
		excluded     []int
		action       string
		removed      []int
		participants []int
	}{
		{"transient", statuses("unhealthy", "unhealthy", "unhealthy", "unhealthy"), nil, "retry", nil, []int{0, 1, 2, 3}},
		{"worker death", statuses("unhealthy", "dead", "unhealthy", "unhealthy"), nil, "scale_down", []int{1}, []int{0, 2, 3}},
		{"multiple deaths", statuses("unhealthy", "dead", "unhealthy", "dead"), nil, "scale_down", []int{1, 3}, []int{0, 2}},
		{"repeated loss", statuses("unhealthy", "dead", "unhealthy", "unknown"), []int{3}, "scale_down", []int{1}, []int{0, 2}},
		{"capacity", statuses("unhealthy", "dead", "dead", "dead"), nil, "reset", nil, nil},
		{"capacity before survivor diagnosis", statuses("healthy", "dead", "dead", "dead"), nil, "reset", nil, nil},
		{"unknown is not lost capacity", statuses("healthy", "unknown", "unknown", "unknown"), nil, "frontend_degraded", nil, []int{0}},
		{"wait for peers", statuses("healthy", "dead", "unhealthy", "diagnosing"), nil, "wait", nil, nil},
		{"frontend only", statuses("healthy", "unknown", "healthy", "healthy"), nil, "frontend_degraded", nil, []int{0, 2, 3}},
		{"missing is not dead", statuses("unknown", "unknown", "unknown", "unknown"), nil, "wait", nil, nil},
		{"excluded stays out", statuses("healthy", "healthy", "healthy", "healthy"), []int{3}, "healthy", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Decide(tt.states, tt.excluded, 2)
			if d.Action != tt.action || !reflect.DeepEqual(d.Removed, tt.removed) || !reflect.DeepEqual(d.Participants, tt.participants) {
				t.Fatalf("unexpected decision: %+v", d)
			}
		})
	}
}

func TestHungRankRequiresUnanimousSurvivorMasks(t *testing.T) {
	s := statuses("unhealthy", "healthy", "unhealthy", "unhealthy")
	for _, i := range []int{0, 2, 3} {
		s[i].Mask = []int{0, 1, 0, 0}
	}
	d := Decide(s, nil, 2)
	if d.Action != "scale_down" || !slices.Equal(d.Removed, []int{1}) {
		t.Fatalf("missing mask-based exclusion: %+v", d)
	}
	s[2].Mask = nil
	if d := Decide(s, nil, 2); d.Action != "wait" {
		t.Fatalf("acted on incomplete diagnosis: %+v", d)
	}
}

func TestMissingFrontendWaitsForEngineDiagnosisBeforeReset(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("healthy", "unknown", "healthy", "healthy")
	adapter.verifyErr = errors.New("collective has not completed")
	step(t, controller)
	state := controller.Snapshot()
	if state.Phase != "diagnosing" || len(state.Admitted) != 0 || len(workload.resets) != 0 {
		t.Fatalf("frontend uncertainty caused premature reset or admission: %+v", state)
	}
	step(t, controller)
	if !controller.Snapshot().Since.Equal(state.Since) {
		t.Fatal("repeated verification restarted the diagnosis deadline")
	}
	adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
	adapter.verifyErr = nil
	ageDiagnosis(controller)
	step(t, controller)
	if controller.Snapshot().Instruction != "scale_down" || len(adapter.dispatches) != 1 || len(workload.resets) != 0 {
		t.Fatal("diagnosed engine loss did not proceed to FT")
	}
}

func TestUnresolvedFrontendFaultEventuallyResets(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("healthy", "unknown", "healthy", "healthy")
	adapter.verifyErr = errors.New("collective stalled")
	step(t, controller)
	state := controller.Snapshot()
	state.Since = time.Now().Add(-2 * time.Minute)
	controller.set(state)
	step(t, controller)
	if controller.Snapshot().Phase != "resetting" || len(workload.resets) != 1 {
		t.Fatal("unresolved inference failure did not reach bounded reset")
	}
}

func TestIdlePeersReceiveOneDiagnosisProbeWithAdmissionClosed(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("healthy", "dead", "healthy", "healthy")
	adapter.verifyErr = errors.New("peer collective timed out")
	step(t, controller)
	step(t, controller)
	if adapter.verifications != 2 || controller.Snapshot().Phase != "diagnosing" ||
		len(controller.Snapshot().Admitted) != 0 || len(workload.resets) != 0 {
		t.Fatal("idle diagnosis probe was repeated, reopened admission, or forced an early reset")
	}
	adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
	ageDiagnosis(controller)
	step(t, controller)
	if len(adapter.dispatches) != 1 {
		t.Fatal("did not dispatch FT after peer diagnosis")
	}
}

type fakeAdapter struct {
	engine.Adapter
	status                 []engine.RankStatus
	dispatches             []engine.Round
	verifyErr, dispatchErr error
	verifications          int
}

func (f *fakeAdapter) EngineStatus(context.Context, engine.Group) []engine.RankStatus {
	return f.status
}

//nolint:gocritic // Implements the immutable round value required by the adapter interface.
func (f *fakeAdapter) ResumeEngine(_ context.Context, _ engine.Group, r engine.Round) ([]engine.Acceptance, error) {
	f.dispatches = append(f.dispatches, r)
	return nil, f.dispatchErr
}

//nolint:gocritic // Implements the immutable round value required by the adapter interface.
func (f *fakeAdapter) ScaleDown(ctx context.Context, g engine.Group, r engine.Round) ([]engine.Acceptance, error) {
	return f.ResumeEngine(ctx, g, r)
}

func (f *fakeAdapter) Verify(context.Context, engine.Group, []int) error {
	f.verifications++
	return f.verifyErr
}

type fakeWorkload struct {
	group          engine.Group
	saved          []byte
	saveErr        error
	discoverErrors []error
	resets         []engine.Group
}

func (f *fakeWorkload) Discover(context.Context) (engine.Group, error) {
	if len(f.discoverErrors) > 0 {
		err := f.discoverErrors[0]
		f.discoverErrors = f.discoverErrors[1:]
		if err != nil {
			return engine.Group{}, err
		}
	}

	return f.group, nil
}

func (f *fakeWorkload) Reset(_ context.Context, g engine.Group) error {
	f.resets = append(f.resets, g)
	return nil
}
func (f *fakeWorkload) Load(context.Context) ([]byte, error) { return f.saved, nil }
func (f *fakeWorkload) Save(_ context.Context, b []byte) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = slices.Clone(b)
	return nil
}

func setup(t *testing.T) (*Controller, *fakeAdapter, *fakeWorkload) {
	t.Helper()
	g := engine.Group{ID: "original", Endpoints: []engine.Endpoint{
		{Rank: 0, URL: "http://a:8000", Pod: "test-0", PodUID: "a"},
		{Rank: 1, URL: "http://a:8001", Pod: "test-0", PodUID: "a"},
		{Rank: 2, URL: "http://b:8000", Pod: "test-0-1", PodUID: "b"},
		{Rank: 3, URL: "http://b:8001", Pod: "test-0-1", PodUID: "b"},
	}}
	adapter := &fakeAdapter{status: statuses("healthy", "healthy", "healthy", "healthy")}
	workload := &fakeWorkload{group: g}
	controller, err := New(context.Background(), adapter, workload, Defaults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	return controller, adapter, workload
}

func ageDiagnosis(controller *Controller) {
	s := controller.Snapshot()
	s.Since = time.Now().Add(-5 * time.Second)
	controller.set(s)
}

func TestScaleDownClosesRoutingAndRequiresInference(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
	if err := controller.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(controller.Snapshot().Admitted) != 0 {
		t.Fatal("traffic admitted during diagnosis")
	}
	ageDiagnosis(controller)
	if err := controller.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := controller.Snapshot()
	if s.Phase != "applying" || len(adapter.dispatches) != 1 || len(s.Admitted) != 0 {
		t.Fatalf("bad dispatch: %+v", s)
	}
	adapter.status = statuses("healthy", "dead", "healthy", "healthy")
	if err := controller.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	s = controller.Snapshot()
	if s.Phase != "serving" || !slices.Equal(s.Excluded, []int{1}) ||
		!slices.Equal(s.Admitted, []int{0, 2, 3}) || adapter.verifications != 2 || len(workload.resets) != 0 {
		t.Fatalf("bad completion: %+v", s)
	}
	// The excluded API returning healthy must not automatically rejoin.
	adapter.status = statuses("healthy", "healthy", "healthy", "healthy")
	step(t, controller)
	if slices.Contains(controller.Snapshot().Admitted, 1) {
		t.Fatal("excluded rank rejoined")
	}
}

func TestPersistedDispatchIsNeverReplayedAfterRestart(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
	step(t, controller)
	ageDiagnosis(controller)
	step(t, controller)
	before := controller.Snapshot().Round
	replacement, err := New(context.Background(), adapter, workload, Defaults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(adapter.dispatches) != 1 || replacement.Snapshot().Round.ID != before.ID {
		t.Fatal("replacement duplicated the collective command")
	}
	adapter.status = statuses("healthy", "dead", "healthy", "healthy")
	step(t, replacement)
	if replacement.Snapshot().Phase != "serving" {
		t.Fatal("did not reconcile completed round")
	}
}

func TestRemovingCurrentStoreMasterSuppliesReplacementAfterEarlierExclusion(t *testing.T) {
	controller, adapter, _ := setup(t)
	state := controller.Snapshot()
	state.Excluded = []int{0}
	state.Admitted = []int{1, 2, 3}
	controller.set(state)
	adapter.status = statuses("unknown", "dead", "unhealthy", "unhealthy")
	step(t, controller)
	ageDiagnosis(controller)
	step(t, controller)
	round := controller.Snapshot().Round
	if !slices.Equal(round.Participants, []int{2, 3}) || !slices.Equal(round.Removed, []int{1}) ||
		round.MasterIP != "b" || round.StorePort != controller.Config.StorePort {
		t.Fatalf("missing replacement for current master rank 1: %+v", round)
	}
}

func TestDiscoveryFailurePreservesFaultReconciliation(t *testing.T) {
	for _, phase := range []string{"diagnosing", "applying"} {
		t.Run(phase, func(t *testing.T) {
			controller, adapter, workload := setup(t)
			adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
			step(t, controller)
			ageDiagnosis(controller)
			if phase == "applying" {
				step(t, controller)
			}
			before := controller.Snapshot()
			workload.discoverErrors = []error{errors.New("temporary Kubernetes read failure")}
			step(t, controller)
			after := controller.Snapshot()
			if after.Phase != phase || !after.Since.Equal(before.Since) ||
				!reflect.DeepEqual(after.Round, before.Round) || len(after.Admitted) != 0 || len(workload.resets) != 0 {
				t.Fatalf("discovery uncertainty changed fault reconciliation: %+v", after)
			}
			// The saved uncertainty must also preserve the round across a restart.
			replacement, err := New(context.Background(), adapter, workload, Defaults(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "applying" {
				adapter.status = statuses("healthy", "dead", "healthy", "healthy")
			}
			step(t, replacement)
			want := "applying"
			if phase == "applying" {
				want = "serving"
			}
			if replacement.Snapshot().Phase != want || len(adapter.dispatches) != 1 {
				t.Fatalf("unchanged group did not resume without replay: %+v", replacement.Snapshot())
			}
		})
	}
}

func TestDiscoveryFailureDuringVerificationPreservesRound(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
	step(t, controller)
	ageDiagnosis(controller)
	step(t, controller)
	before := controller.Snapshot()
	adapter.status = statuses("healthy", "dead", "healthy", "healthy")
	workload.discoverErrors = []error{nil, errors.New("post-inference Kubernetes read failure")}
	step(t, controller)
	after := controller.Snapshot()
	if after.Phase != "applying" || !after.Since.Equal(before.Since) ||
		!reflect.DeepEqual(after.Round, before.Round) || len(after.Admitted) != 0 {
		t.Fatalf("verification read failure discarded round: %+v", after)
	}
	step(t, controller)
	if controller.Snapshot().Phase != "serving" || len(adapter.dispatches) != 1 ||
		!slices.Equal(controller.Snapshot().Excluded, []int{1}) {
		t.Fatal("completed round did not reverify after discovery resumed")
	}
}

func TestDiscoveryFailureDoesNotExtendFTDeadline(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
	step(t, controller)
	ageDiagnosis(controller)
	step(t, controller)
	state := controller.Snapshot()
	state.Since = time.Now().Add(-2 * controller.Config.ApplyTimeout)
	controller.set(state)
	workload.discoverErrors = []error{errors.New("temporary Kubernetes read failure")}
	step(t, controller)
	step(t, controller)
	if controller.Snapshot().Phase != "resetting" || len(workload.resets) != 1 || len(adapter.dispatches) != 1 {
		t.Fatal("discovery failure suppressed the expired FT deadline")
	}
}

func TestDiscoveryFailureClosesAdmissionBeforePersistence(t *testing.T) {
	controller, adapter, workload := setup(t)
	workload.discoverErrors = []error{errors.New("temporary Kubernetes read failure")}
	workload.saveErr = errors.New("Kubernetes state write also unavailable")
	if err := controller.Step(context.Background()); err == nil {
		t.Fatal("state persistence failure was hidden")
	}
	state := controller.Snapshot()
	if state.Phase != "serving" || len(state.Admitted) != 0 || state.DiscoveryUnavailableSince.IsZero() {
		t.Fatalf("uncertain identity left admission open or changed phase: %+v", state)
	}
	workload.saveErr = nil
	step(t, controller)
	state = controller.Snapshot()
	if len(state.Admitted) != 4 || adapter.verifications != 2 || !state.DiscoveryUnavailableSince.IsZero() {
		t.Fatal("same healthy group was not reverified after discovery resumed")
	}
}

func TestProlongedDiscoveryUncertaintyEscalatesWithoutDeletingUnknownWorkload(t *testing.T) {
	controller, adapter, workload := setup(t)
	state := controller.Snapshot()
	state.DiscoveryUnavailableSince = time.Now().Add(-2 * controller.Config.StartupTimeout)
	controller.set(state)
	workload.discoverErrors = []error{errors.New("Kubernetes discovery still unavailable")}
	step(t, controller)
	if controller.Snapshot().Phase != "intervention_required" || len(workload.resets) != 0 ||
		len(adapter.dispatches) != 0 || len(controller.Snapshot().Admitted) != 0 {
		t.Fatal("unbounded discovery uncertainty or action against an unconfirmed workload")
	}
}

func TestRepeatedPostVerificationReadFailureKeepsUncertaintyDeadline(t *testing.T) {
	controller, _, workload := setup(t)
	state := controller.Snapshot()
	state.Admitted = nil
	state.DiscoveryUnavailableSince = time.Now().Add(-2 * controller.Config.StartupTimeout)
	controller.set(state)
	// A successful initial read does not resolve the uncertainty when the
	// identity check after inference keeps failing.
	workload.discoverErrors = []error{nil, errors.New("post-inference Kubernetes read failure")}
	step(t, controller)
	if controller.Snapshot().Phase != "intervention_required" || len(workload.resets) != 0 ||
		len(controller.Snapshot().Admitted) != 0 {
		t.Fatal("successful preliminary read restarted an unresolved verification deadline")
	}
}

func TestPersistenceFailurePreventsDispatch(t *testing.T) {
	controller, adapter, workload := setup(t)
	adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
	step(t, controller)
	ageDiagnosis(controller)
	workload.saveErr = errors.New("storage unavailable")
	if err := controller.Step(context.Background()); err == nil {
		t.Fatal("persistence failure ignored")
	}
	if len(adapter.dispatches) != 0 || len(controller.Snapshot().Admitted) != 0 {
		t.Fatal("dispatched or admitted without durable intent")
	}
}

func TestFailureAndTimeoutHandOffToReset(t *testing.T) {
	for _, kind := range []string{"rejected", "failed", "deadline", "inference"} {
		t.Run(kind, func(t *testing.T) {
			controller, adapter, workload := setup(t)
			adapter.status = statuses("unhealthy", "dead", "unhealthy", "unhealthy")
			step(t, controller)
			ageDiagnosis(controller)
			if kind == "rejected" {
				adapter.dispatchErr = errors.New("HTTP rejection")
			}
			step(t, controller)
			s := controller.Snapshot()
			switch kind {
			case "failed":
				adapter.status[0].FTState = "failed"
				adapter.status[0].FTError = "collective failed"
			case "deadline":
				s.Since = time.Now().Add(-2 * time.Minute)
				controller.set(s)
			case "inference":
				adapter.status = statuses("healthy", "dead", "healthy", "healthy")
				adapter.verifyErr = errors.New("no inference")
			}
			if kind != "rejected" {
				step(t, controller)
			}
			if controller.Snapshot().Phase != "resetting" || len(workload.resets) != 1 || len(controller.Snapshot().Admitted) != 0 {
				t.Fatalf("failure did not reset: %+v", controller.Snapshot())
			}
			if workload.resets[0].ID != "original" {
				t.Fatal("reset targeted a new group")
			}
		})
	}
}

func TestAdmissionFencesOldPodIdentity(t *testing.T) {
	controller, _, _ := setup(t)
	for _, tt := range []struct {
		uid  string
		want bool
	}{{"a", true}, {"replaced-pod", false}} {
		writer := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/admission?rank=0&pod_uid="+tt.uid, http.NoBody)
		controller.Handler().ServeHTTP(writer, request)
		var reply struct{ Admitted bool }
		if err := json.Unmarshal(writer.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Admitted != tt.want {
			t.Fatalf("UID %s admission=%v", tt.uid, reply.Admitted)
		}
	}
}

func step(t *testing.T, controller *Controller) {
	t.Helper()
	if err := controller.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
}
