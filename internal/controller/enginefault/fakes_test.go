// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

//nolint:testpackage // Tests drive the clock through the unexported load.
package enginefault

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

type fakeWorkload struct {
	group       workload.Group
	discoverErr error
	resetErr    error
	releaseErr  error
	discovers   int
	resets      []workload.PodRef
	resetAlls   [][]workload.PodRef
	releases    [][]workload.PodRef
}

func (fake *fakeWorkload) Discover(context.Context) (workload.Group, error) {
	fake.discovers++
	group := fake.group
	group.Pods = slices.Clone(fake.group.Pods)
	return group, fake.discoverErr
}

func (fake *fakeWorkload) Reset(_ context.Context, pod workload.PodRef) error {
	fake.resets = append(fake.resets, pod)
	return fake.resetErr
}

func (fake *fakeWorkload) ResetAll(_ context.Context, pods []workload.PodRef) error {
	fake.resetAlls = append(fake.resetAlls, pods)
	return nil
}

func (fake *fakeWorkload) Release(_ context.Context, pods []workload.PodRef) error {
	fake.releases = append(fake.releases, pods)
	return fake.releaseErr
}

type fakeEngine struct {
	grow       bool
	members    []engine.Member
	plan       engine.Plan
	progress   engine.Progress
	applyErr   error
	verifyErr  error
	hangVerify bool
	probes     int
	applied    []engine.Round
	verified   [][]engine.MemberID
	admitted   []engine.MemberID
	candidates []engine.MemberID
	onApply    func()
}

func (fake *fakeEngine) Capabilities() engine.Capabilities {
	return engine.Capabilities{Grow: fake.grow}
}

func (fake *fakeEngine) Observe(_ context.Context, pods []workload.Pod) engine.Observation {
	var obs engine.Observation
	for index := range fake.members {
		member := fake.members[index]
		if slices.ContainsFunc(pods, func(pod workload.Pod) bool {
			return pod.UID == member.Pod.UID && pod.Identity == member.PodIdentity && pod.State == workload.PodReady
		}) {
			obs.Members = append(obs.Members, member)
		}
	}
	return obs
}

func (fake *fakeEngine) Probe(context.Context, engine.Observation) error {
	fake.probes++
	return nil
}

//nolint:gocritic // The interface passes the round by value.
func (fake *fakeEngine) Apply(_ context.Context, _ engine.Observation, round engine.Round) ([]engine.Acceptance, error) {
	if fake.onApply != nil {
		fake.onApply()
	}
	fake.applied = append(fake.applied, round)
	return nil, fake.applyErr
}

func (fake *fakeEngine) Verify(ctx context.Context, _ engine.Observation, members []engine.MemberID) error {
	fake.verified = append(fake.verified, slices.Clone(members))
	if fake.hangVerify {
		<-ctx.Done()
		return ctx.Err()
	}
	return fake.verifyErr
}

func (fake *fakeEngine) Plan(_ engine.Observation, admitted, candidates []engine.MemberID) engine.Plan {
	fake.admitted, fake.candidates = admitted, candidates
	return fake.plan
}

func (fake *fakeEngine) Progress(engine.Observation, engine.Round) engine.Progress {
	return fake.progress
}

type fakeJournal struct {
	data     []byte
	loadErrs []error
	saveErrs []error
	loads    int
	saves    int
}

func (fake *fakeJournal) Load(context.Context) ([]byte, error) {
	fake.loads++
	if len(fake.loadErrs) > 0 {
		err := fake.loadErrs[0]
		fake.loadErrs = fake.loadErrs[1:]
		return nil, err
	}
	return slices.Clone(fake.data), nil
}

func (fake *fakeJournal) Save(_ context.Context, data []byte) error {
	if len(fake.saveErrs) > 0 {
		err := fake.saveErrs[0]
		fake.saveErrs = fake.saveErrs[1:]
		if err != nil {
			return err
		}
	}
	fake.saves++
	fake.data = slices.Clone(data)
	return nil
}

type fakeRecorder struct {
	calls []string
}

func (fake *fakeRecorder) add(format string, args ...any) {
	fake.calls = append(fake.calls, fmt.Sprintf(format, args...))
}

func (*fakeRecorder) Stepped(*Record, error) {}

func (fake *fakeRecorder) PhaseChanged(from, to Phase, _ string) {
	fake.add("PhaseChanged %s->%s", from, to)
}

func (fake *fakeRecorder) IncidentOpened(incident *Incident) {
	fake.add("IncidentOpened %v %v", incident.Affected, incident.Nodes)
}

func (fake *fakeRecorder) RoundStarted(round *engine.Round, _ *engine.Plan) {
	fake.add("RoundStarted remove=%v add=%v", round.Remove, round.Add)
}

func (fake *fakeRecorder) RoundFinished(_ *engine.Round, progress engine.Progress) {
	fake.add("RoundFinished %s", progress.State)
}

func (fake *fakeRecorder) PodResetRequested(pod workload.PodRef, _ error) {
	fake.add("PodResetRequested %s", pod.Name)
}

func (fake *fakeRecorder) ResetAllRequested(reason ResetReason, pods []workload.PodRef) {
	fake.add("ResetAllRequested %s %d", reason, len(pods))
}

func (fake *fakeRecorder) ReleaseRequested(pods []workload.PodRef, _ error) {
	fake.add("ReleaseRequested %d", len(pods))
}

func (fake *fakeRecorder) SaveFailed(error) { fake.add("SaveFailed") }

func (fake *fakeRecorder) IncidentClosed(incident *Incident) {
	fake.add("IncidentClosed %s", incident.Outcome)
}

func (*fakeRecorder) Ready() error { return nil }

type harness struct {
	t      *testing.T
	wl     *fakeWorkload
	eng    *fakeEngine
	store  *fakeJournal
	rec    *fakeRecorder
	config Config
	clock  time.Time
	fsm    *controller
}

var names = []string{"lws-0", "lws-0-1", "lws-0-2", "lws-0-3"}

func ids(list ...string) []engine.MemberID {
	result := make([]engine.MemberID, 0, len(list))
	for _, name := range list {
		result = append(result, engine.MemberID(name))
	}
	return result
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	test := &harness{
		t: t, wl: &fakeWorkload{}, eng: &fakeEngine{grow: true, plan: engine.Plan{Kind: engine.Healthy}},
		store: &fakeJournal{}, rec: &fakeRecorder{}, config: Defaults(),
		clock: time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC),
	}
	for index, name := range names {
		letter := string(rune('A' + index))
		uid, identity := string(rune('a'+index))+"1", "id"+letter
		test.wl.group.Pods = append(test.wl.group.Pods, workload.Pod{
			Name: name, UID: uid, IP: fmt.Sprintf("10.0.0.%d", index), Index: index, Identity: identity,
			Node: fmt.Sprintf("gpu-node-%d", index+1), State: workload.PodReady,
		})
		test.eng.members = append(test.eng.members, engine.Member{
			Pod: workload.PodRef{Name: name, UID: uid}, PodIdentity: identity, Name: fmt.Sprintf("dp%d", index), Serving: true,
		})
	}
	return test
}

func (test *harness) load() {
	test.t.Helper()
	fsm, err := load(test.t.Context(), test.wl, test.eng, test.store, test.rec, test.config, func() time.Time { return test.clock })
	if err != nil {
		test.t.Fatal(err)
	}
	test.fsm = fsm
}

func (test *harness) pollErr() error {
	test.clock = test.clock.Add(test.config.PollInterval)
	return test.fsm.Step(test.t.Context())
}

func (test *harness) pollAnyway() {
	if err := test.pollErr(); err != nil {
		test.t.Log(err)
	}
}

func (test *harness) poll() {
	test.t.Helper()
	if err := test.pollErr(); err != nil {
		test.t.Fatal(err)
	}
}

func (test *harness) serving() {
	test.t.Helper()
	test.load()
	test.poll()
	if record := test.record(); record.Phase != Serving || len(record.Members) != 4 {
		test.t.Fatalf("not serving: %+v", record)
	}
	test.rec.calls = nil
}

func (test *harness) record() Record {
	test.t.Helper()
	var record Record
	if err := json.Unmarshal(test.store.data, &record); err != nil {
		test.t.Fatal(err)
	}
	return record
}

func (test *harness) member(name string) *engine.Member {
	index := slices.IndexFunc(test.eng.members, func(member engine.Member) bool { return member.Name == name })
	return &test.eng.members[index]
}

func (test *harness) dead() {
	for index := range test.eng.members {
		test.eng.members[index].Serving = false
	}
	test.member("dp3").Terminal = true
}

func (test *harness) shrinkPlan() engine.Plan {
	return engine.Plan{
		Kind: engine.Change, Participants: ids("dp0@idA", "dp1@idB", "dp2@idC"), Remove: ids("dp3@idD"), Reason: "dp3 is dead",
	}
}

func (test *harness) wantCalls(want ...string) {
	test.t.Helper()
	if !slices.Equal(test.rec.calls, want) {
		test.t.Fatalf("recorder calls:\n got %q\nwant %q", test.rec.calls, want)
	}
	test.rec.calls = nil
}
