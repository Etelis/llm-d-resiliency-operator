// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
	"github.com/llm-d/llm-d-resiliency-operator/internal/journal"
	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

type controller struct {
	wl       workload.Workload
	eng      engine.Engine
	store    journal.Journal
	recorder Recorder
	config   Config
	now      func() time.Time
	saved    []byte
	pending  []func()
	// callErr collects workload errors that do not stop the step: the call is repeated next poll.
	callErr error
	// waited restarts the deadlines on the first poll after a wait for the workload, or after a load.
	waited bool
}

type view struct {
	group      *workload.Group
	obs        engine.Observation
	admitted   []engine.MemberID
	candidates []engine.MemberID
	members    map[engine.MemberID]*engine.Member
}

func New(ctx context.Context, wl workload.Workload, eng engine.Engine, store journal.Journal,
	recorder Recorder, config Config,
) (Controller, error) {
	fsm, err := load(ctx, wl, eng, store, recorder, config, time.Now)
	if err != nil {
		return nil, err
	}
	return fsm, nil
}

func load(ctx context.Context, wl workload.Workload, eng engine.Engine, store journal.Journal,
	recorder Recorder, config Config, now func() time.Time,
) (*controller, error) {
	data, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}
	var record Record
	if len(data) > 0 {
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("invalid journal record: %w", err)
		}
	}
	if !slices.Contains([]Phase{Starting, Serving, Recovering}, record.Phase) {
		record = Record{Phase: Starting, Since: now().UTC(), Reason: "new group"}
	}
	saved, err := json.Marshal(&record)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		saved = nil
	}
	return &controller{
		wl: wl, eng: eng, store: store, recorder: recorder, config: config, now: now, saved: saved, waited: true,
	}, nil
}

func (fsm *controller) Step(ctx context.Context) error {
	fsm.pending, fsm.callErr = nil, nil
	next := fsm.record()
	err := fsm.step(ctx, &next)
	if err == nil {
		err = fsm.save(ctx, &next)
	}
	err = errors.Join(err, fsm.callErr)
	current := fsm.record()
	fsm.recorder.Stepped(&current, err)
	return err
}

func (fsm *controller) record() Record {
	var record Record
	if len(fsm.saved) == 0 {
		return Record{Phase: Starting, Since: fsm.now().UTC(), Reason: "new group"}
	}
	if err := json.Unmarshal(fsm.saved, &record); err != nil {
		panic(err)
	}
	return record
}

func (fsm *controller) save(ctx context.Context, next *Record) error {
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, fsm.saved) {
		if err := fsm.store.Save(ctx, data); err != nil {
			fsm.pending = nil
			fsm.recorder.SaveFailed(err)
			return fmt.Errorf("save journal: %w", err)
		}
		previous := fsm.record()
		fsm.saved = data
		if previous.Phase != next.Phase {
			fsm.recorder.PhaseChanged(previous.Phase, next.Phase, next.Reason)
		}
	}
	for _, emit := range fsm.pending {
		emit()
	}
	fsm.pending = nil
	return nil
}

func (fsm *controller) later(emit func()) { fsm.pending = append(fsm.pending, emit) }

func (fsm *controller) called(err error) { fsm.callErr = errors.Join(fsm.callErr, err) }

func (fsm *controller) step(ctx context.Context, next *Record) error {
	now := fsm.now().UTC()
	group, err := fsm.wl.Discover(ctx)
	if err != nil {
		if next.DiscoveryUnavailableSince.IsZero() {
			next.DiscoveryUnavailableSince = now
			if saveErr := fsm.save(ctx, next); saveErr != nil {
				return saveErr
			}
		}
		return fmt.Errorf("discover: %w", err)
	}
	next.DiscoveryUnavailableSince = time.Time{}
	next.RecreateStopped = group.RecreateStopped
	if waiting, err := fsm.waitForWorkload(ctx, next, &group, now); waiting || err != nil {
		return err
	}
	if next.Phase != Starting && !sameGroup(next, &group) {
		fsm.startOver(next, "new group", now)
		return nil
	}
	if next.Phase == Starting {
		return fsm.start(ctx, next, &group, now)
	}
	if next.Pods == nil {
		next.Pods = map[string]PodRecord{}
	}
	fsm.comparePods(ctx, next, &group)
	current := fsm.connect(ctx, next, &group)
	if reason := violation(next, current); reason != "" {
		return fsm.resetAll(ctx, next, ResetExclusionBroken, reason, now)
	}
	switch {
	case next.Round != nil:
		return fsm.watch(ctx, next, current, now)
	case next.Phase == Serving:
		return fsm.serve(ctx, next, current, now)
	}
	return fsm.recover(ctx, next, current, now)
}

func (fsm *controller) waitForWorkload(ctx context.Context, next *Record, group *workload.Group, now time.Time) (bool, error) {
	owed := len(next.Pods) > 0
	present := false
	for uid, pod := range next.Pods {
		owed = owed && pod.Removing
		present = present || find(group, uid) != nil
	}
	owed = owed && present
	if !group.Recreating && !group.RecreateStopped && !owed {
		if fsm.waited && next.Phase == Recovering {
			next.Since = now
		}
		if fsm.waited && next.Round != nil {
			next.RoundSince = now
		}
		fsm.waited = false
		return false, nil
	}
	fsm.waited = true
	if (group.Recreating || group.RecreateStopped) && next.Phase == Serving && next.Incident == nil {
		cause := "workload is recreating the group"
		if group.RecreateStopped {
			cause = "workload stopped recreating the group"
		}
		fsm.openIncident(next, cause, nil, now)
	}
	if err := fsm.save(ctx, next); err != nil || !owed {
		return true, err
	}
	fsm.called(fsm.wl.ResetAll(ctx, refs(next)))
	return true, nil
}

func sameGroup(next *Record, group *workload.Group) bool {
	for uid, saved := range next.Pods {
		if pod := find(group, uid); pod != nil && pod.Identity == saved.Identity && pod.State != workload.PodTerminating {
			return true
		}
	}
	return false
}

func (fsm *controller) startOver(next *Record, reason string, now time.Time) {
	if next.Round != nil {
		fsm.finishRound(next, &engine.Progress{State: engine.Running, Reason: reason}, now)
	}
	fsm.closeIncident(next, Recreated, now)
	*next = Record{Phase: Starting, Since: now, Reason: reason, RecreateStopped: next.RecreateStopped}
}

func (fsm *controller) start(ctx context.Context, next *Record, group *workload.Group, now time.Time) error {
	next.Pods = map[string]PodRecord{}
	for index := range group.Pods {
		if pod := &group.Pods[index]; pod.State != workload.PodTerminating {
			next.Pods[pod.UID] = PodRecord{Name: pod.Name, Identity: pod.Identity, Node: pod.Node}
		}
	}
	obs := fsm.eng.Observe(ctx, group.Pods)
	for index := range group.Pods {
		pod := &group.Pods[index]
		hasMember := slices.ContainsFunc(obs.Members, func(member engine.Member) bool { return member.Pod.UID == pod.UID })
		if pod.State == workload.PodStarting || (pod.State == workload.PodReady && !hasMember) {
			return nil
		}
	}
	var reported, live []engine.MemberID
	for index := range obs.Members {
		member := &obs.Members[index]
		reported = append(reported, member.ID())
		if !member.Terminal {
			live = append(live, member.ID())
		}
	}
	slices.Sort(live)
	plan := fsm.eng.Plan(obs, live, nil)
	if plan.Kind != engine.Healthy {
		return nil
	}
	next.Members = map[engine.MemberID]MemberState{}
	for _, id := range reported {
		next.Members[id] = Admitted
	}
	enter(next, Serving, plan.Reason, now)
	return nil
}

func (fsm *controller) comparePods(ctx context.Context, next *Record, group *workload.Group) {
	for _, uid := range slices.Sorted(maps.Keys(next.Pods)) {
		saved, pod := next.Pods[uid], find(group, uid)
		switch {
		case saved.Removing && pod == nil:
			forget(next, uid)
		case saved.Removing && pod.State != workload.PodTerminating && fsm.config.CapacityRestore == RestoreResetPod:
			if err := fsm.wl.Reset(ctx, podRef(uid, &saved)); errors.Is(err, workload.ErrUnsupported) {
				saved.Removing = false
				next.Pods[uid] = saved
			} else {
				fsm.called(err)
			}
		case saved.Removing:
		case pod == nil && !tracks(next, saved.Identity):
			delete(next.Pods, uid)
		case pod != nil && pod.Identity != saved.Identity && !tracks(next, saved.Identity):
			next.Pods[uid] = PodRecord{Name: pod.Name, Identity: pod.Identity, Node: pod.Node}
		}
	}
	for index := range group.Pods {
		pod := &group.Pods[index]
		if _, ok := next.Pods[pod.UID]; !ok && pod.State != workload.PodTerminating {
			next.Pods[pod.UID] = PodRecord{Name: pod.Name, Identity: pod.Identity, Node: pod.Node}
		}
	}
}

func (fsm *controller) connect(ctx context.Context, next *Record, group *workload.Group) *view {
	current := &view{group: group, obs: fsm.eng.Observe(ctx, group.Pods), members: map[engine.MemberID]*engine.Member{}}
	if next.Members == nil {
		next.Members = map[engine.MemberID]MemberState{}
	}
	for index := range current.obs.Members {
		member := &current.obs.Members[index]
		id := member.ID()
		state, tracked := next.Members[id]
		lost := tracked && isLost(next, group, id)
		switch {
		case lost && state == Candidate:
			next.Members[id] = Rejected
		case lost:
			member.Terminal, member.Serving = true, false
		case tracked && state == Candidate && member.Terminal:
			next.Members[id] = Rejected
		case !tracked && joinable(next, group, member):
			next.Members[id] = Candidate
		}
		current.members[id] = member
	}
	for _, id := range slices.Sorted(maps.Keys(next.Members)) {
		if _, reported := current.members[id]; reported || !isLost(next, group, id) {
			continue
		}
		switch next.Members[id] {
		case Admitted:
			current.obs.Members = append(current.obs.Members, lostMember(next, id))
		case Candidate:
			next.Members[id] = Rejected
		}
	}
	for index := range current.obs.Members {
		member := &current.obs.Members[index]
		current.members[member.ID()] = member
	}
	for _, id := range slices.Sorted(maps.Keys(next.Members)) {
		switch next.Members[id] {
		case Admitted:
			current.admitted = append(current.admitted, id)
		case Candidate:
			current.candidates = append(current.candidates, id)
		}
	}
	return current
}

func joinable(next *Record, group *workload.Group, member *engine.Member) bool {
	saved, ok := next.Pods[member.Pod.UID]
	pod := find(group, member.Pod.UID)
	return ok && !saved.Removing && saved.Identity == member.PodIdentity && pod != nil &&
		pod.State == workload.PodReady && pod.Identity == member.PodIdentity
}

func isLost(next *Record, group *workload.Group, id engine.MemberID) bool {
	uid, saved, ok := podOf(next, id)
	if !ok {
		return true
	}
	pod := find(group, uid)
	return pod == nil || pod.State == workload.PodTerminating || pod.Identity != saved.Identity
}

func lostMember(next *Record, id engine.MemberID) engine.Member {
	name, identity := nameOf(id), identityOf(id)
	uid, saved, _ := podOf(next, id)
	return engine.Member{
		Pod: workload.PodRef{Name: saved.Name, UID: uid}, PodIdentity: identity, Name: name,
		Terminal: true, Detail: "pod lost",
	}
}

func violation(next *Record, current *view) string {
	for id, member := range current.members {
		state := next.Members[id]
		switch {
		case state == Excluded && !member.Terminal:
			return fmt.Sprintf("C4: excluded member %s is not terminal", id)
		case state != Admitted && member.Serving && (next.Round == nil || !slices.Contains(next.Round.Add, id)):
			return fmt.Sprintf("C5: member %s serves before it is admitted", id)
		case next.Round != nil && slices.Contains(next.Round.Remove, id) && !member.Terminal:
			return fmt.Sprintf("C1: member %s left the round but is not terminal", id)
		}
	}
	return ""
}

func (fsm *controller) serve(ctx context.Context, next *Record, current *view, now time.Time) error {
	plan := fsm.eng.Plan(current.obs, current.admitted, current.candidates)
	grow := plan.Kind == engine.Change && len(plan.Remove) == 0 && len(plan.Add) > 0
	switch {
	case plan.Kind != engine.Healthy && !grow:
		return fsm.enterRecovering(ctx, next, current, &plan, now)
	case len(next.Verifying) > 0:
		return fsm.verify(ctx, next, current, now)
	case grow:
		return fsm.send(ctx, next, current, &plan, now)
	}
	fsm.closeIncident(next, outcome(next.Incident), now)
	return fsm.restore(ctx, next, current, now)
}

func (fsm *controller) enterRecovering(ctx context.Context, next *Record, current *view, plan *engine.Plan, now time.Time) error {
	next.Verifying, next.VerifyFailedSince = nil, time.Time{}
	enter(next, Recovering, plan.Reason, now)
	if next.Incident == nil {
		fsm.openIncident(next, plan.Reason, current, now)
	}
	if err := fsm.save(ctx, next); err != nil {
		return err
	}
	if err := fsm.eng.Probe(ctx, current.obs); err != nil {
		next.Reason += ": " + err.Error()
	}
	return nil
}

func (fsm *controller) recover(ctx context.Context, next *Record, current *view, now time.Time) error {
	plan := fsm.eng.Plan(current.obs, current.admitted, current.candidates)
	switch {
	case plan.Kind == engine.Healthy:
		enter(next, Serving, plan.Reason, now)
		fsm.closeIncident(next, outcome(next.Incident), now)
		return nil
	case plan.Kind == engine.Unrecoverable:
		return fsm.resetAll(ctx, next, ResetUnrecoverable, plan.Reason, now)
	case now.Sub(next.Since) > fsm.config.DiagnosisTimeout:
		return fsm.resetAll(ctx, next, ResetDiagnosisDeadline, plan.Reason, now)
	case plan.Kind != engine.Change || now.Sub(next.Since) < 2*fsm.config.PollInterval:
		return nil
	}
	return fsm.send(ctx, next, current, &plan, now)
}

func enter(next *Record, phase Phase, reason string, now time.Time) {
	if next.Phase != phase {
		next.Since = now
	}
	next.Phase, next.Reason = phase, reason
}

func find(group *workload.Group, uid string) *workload.Pod {
	index := slices.IndexFunc(group.Pods, func(pod workload.Pod) bool { return pod.UID == uid })
	if index < 0 {
		return nil
	}
	return &group.Pods[index]
}

func podOf(next *Record, id engine.MemberID) (string, PodRecord, bool) {
	identity := identityOf(id)
	for uid, saved := range next.Pods {
		if saved.Identity == identity {
			return uid, saved, true
		}
	}
	return "", PodRecord{}, false
}

func identityOf(id engine.MemberID) string {
	return string(id)[strings.LastIndex(string(id), "@")+1:]
}

func nameOf(id engine.MemberID) string {
	return strings.TrimSuffix(string(id), "@"+identityOf(id))
}

func tracks(next *Record, identity string) bool {
	for id := range next.Members {
		if identityOf(id) == identity {
			return true
		}
	}
	return false
}

func forget(next *Record, uid string) {
	identity := next.Pods[uid].Identity
	delete(next.Pods, uid)
	for id := range next.Members {
		if identityOf(id) == identity {
			delete(next.Members, id)
		}
	}
}

func refs(next *Record) []workload.PodRef {
	pods := make([]workload.PodRef, 0, len(next.Pods))
	for _, uid := range slices.Sorted(maps.Keys(next.Pods)) {
		saved := next.Pods[uid]
		pods = append(pods, podRef(uid, &saved))
	}
	slices.SortFunc(pods, func(left, right workload.PodRef) int { return strings.Compare(left.Name, right.Name) })
	return pods
}

func podRef(uid string, saved *PodRecord) workload.PodRef {
	return workload.PodRef{Name: saved.Name, UID: uid}
}

func newID(prefix string, size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(value)
}
