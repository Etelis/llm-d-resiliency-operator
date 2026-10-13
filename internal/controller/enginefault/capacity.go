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
	if mode == RestoreResetAll {
		for _, state := range next.Members {
			if state == Excluded || state == Rejected {
				return fsm.resetAll(ctx, next, ResetCapacity, "restoring capacity", now)
			}
		}
		return nil
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
	switch {
	case len(targets) == 0:
		return nil
	case mode == RestoreResetPod && !fsm.eng.Capabilities().Grow:
		for _, pod := range targets {
			keep(next, pod.UID)
		}
		return nil
	}
	if err := fsm.save(ctx, next); err != nil {
		return err
	}
	fsm.remove(ctx, next, targets, true)
	return nil
}

// remove names every Pod in one call. If the workload cannot handle them together, it asks for each alone,
// so a Pod it can never replace does not keep the others below full.
func (fsm *controller) remove(ctx context.Context, next *Record, pods []workload.PodRef, record bool) {
	var err error
	if fsm.config.CapacityRestore == RestoreRelease {
		err = fsm.wl.Release(ctx, pods)
		if record {
			fsm.recorder.ReleaseRequested(pods, err)
		}
	} else {
		err = fsm.wl.Reset(ctx, pods)
		if record {
			fsm.recorder.PodResetRequested(pods, err)
		}
	}
	switch {
	case errors.Is(err, workload.ErrUnsupported) && len(pods) > 1:
		for _, pod := range pods {
			fsm.remove(ctx, next, []workload.PodRef{pod}, record)
		}
	case errors.Is(err, workload.ErrUnsupported):
		keep(next, pods[0].UID)
	default:
		fsm.called(err)
	}
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

func onlyExcluded(next *Record, identity string) bool {
	found := false
	for id, state := range next.Members {
		if identityOf(id) == identity {
			if state != Excluded {
				return false
			}
			found = true
		}
	}
	return found
}
