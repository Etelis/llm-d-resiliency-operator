// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

func (fsm *controller) restore(ctx context.Context, next *Record, current *view, now time.Time) error {
	mode := fsm.config.CapacityRestore
	out := []MemberState{Excluded}
	if mode == RestoreResetAll {
		out = append(out, Rejected)
	}
	var targets []workload.PodRef
	for _, uid := range slices.Sorted(maps.Keys(next.Pods)) {
		saved := next.Pods[uid]
		if saved.Removing || !holdsOnly(next, saved.Identity, out...) {
			continue
		}
		if find(current.group, uid) == nil {
			forget(next, uid)
			continue
		}
		saved.Removing = true
		next.Pods[uid] = saved
		targets = append(targets, podRef(uid, &saved))
	}
	switch {
	case len(targets) == 0:
		return nil
	case mode == RestoreResetAll:
		return fsm.resetAll(ctx, next, ResetCapacity, "restoring capacity", now)
	case mode == RestoreResetPod && !fsm.eng.Capabilities().Grow:
		for _, pod := range targets {
			keep(next, pod.UID)
		}
		return nil
	}
	if err := fsm.save(ctx, next); err != nil {
		return err
	}
	if mode == RestoreRelease {
		err := fsm.wl.Release(ctx, targets)
		fsm.recorder.ReleaseRequested(targets, err)
		for _, pod := range targets {
			fsm.removed(next, pod.UID, err)
		}
		return nil
	}
	for _, pod := range targets {
		err := fsm.wl.Reset(ctx, pod)
		fsm.recorder.PodResetRequested(pod, err)
		fsm.removed(next, pod.UID, err)
	}
	return nil
}

func (fsm *controller) remove(ctx context.Context, pod workload.PodRef) error {
	if fsm.config.CapacityRestore == RestoreRelease {
		return fsm.wl.Release(ctx, []workload.PodRef{pod})
	}
	return fsm.wl.Reset(ctx, pod)
}

func (fsm *controller) removed(next *Record, uid string, err error) {
	if errors.Is(err, workload.ErrUnsupported) {
		keep(next, uid)
		return
	}
	fsm.called(err)
}

// keep leaves a Pod as it is: its members stay out until the next group.
func keep(next *Record, uid string) {
	saved := next.Pods[uid]
	saved.Removing = false
	next.Pods[uid] = saved
	for id, state := range next.Members {
		if identityOf(id) == saved.Identity && state == Excluded {
			next.Members[id] = Rejected
		}
	}
}

func holdsOnly(next *Record, identity string, states ...MemberState) bool {
	found := false
	for id, state := range next.Members {
		if identityOf(id) == identity {
			if !slices.Contains(states, state) {
				return false
			}
			found = true
		}
	}
	return found
}
