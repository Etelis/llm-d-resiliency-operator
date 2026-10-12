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

func (fsm *controller) remove(ctx context.Context, pod workload.PodRef) error {
	if fsm.config.CapacityRestore == RestoreRelease {
		return fsm.wl.Release(ctx, []workload.PodRef{pod})
	}
	return fsm.wl.Reset(ctx, pod)
}

func (fsm *controller) restore(ctx context.Context, next *Record, current *view, now time.Time) error {
	if slices.Contains(slices.Collect(maps.Values(next.Members)), Rejected) {
		return fsm.resetAll(ctx, next, ResetCapacity, "a reset Pod's members could not join", now)
	}
	var targets []workload.PodRef
	for _, uid := range slices.Sorted(maps.Keys(next.Pods)) {
		saved := next.Pods[uid]
		if saved.Removing || !onlyExcluded(next, saved.Identity) {
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
	if len(targets) == 0 {
		return nil
	}
	mode := fsm.config.CapacityRestore
	if mode == RestoreResetAll || (mode == RestoreResetPod && !fsm.eng.Capabilities().Grow) {
		return fsm.resetAll(ctx, next, ResetCapacity, "restoring capacity", now)
	}
	if err := fsm.save(ctx, next); err != nil {
		return err
	}
	if mode == RestoreRelease {
		err := fsm.wl.Release(ctx, targets)
		fsm.recorder.ReleaseRequested(targets, err)
		if errors.Is(err, workload.ErrUnsupported) {
			return nil
		}
		return err
	}
	for _, pod := range targets {
		err := fsm.wl.Reset(ctx, pod)
		fsm.recorder.PodResetRequested(pod, err)
		if errors.Is(err, workload.ErrUnsupported) {
			return fsm.resetAll(ctx, next, ResetCapacity, "the workload cannot reset one Pod", now)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func onlyExcluded(next *Record, identity string) bool {
	found := false
	for id, state := range next.Members {
		if identityOf(id) == identity {
			if state != Excluded && state != Rejected {
				return false
			}
			found = true
		}
	}
	return found
}
