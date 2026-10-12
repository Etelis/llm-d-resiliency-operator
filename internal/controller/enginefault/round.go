// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
)

func (fsm *controller) watch(ctx context.Context, next *Record, current *view, now time.Time) error {
	round := *next.Round
	progress := fsm.eng.Progress(current.obs, round)
	switch progress.State {
	case engine.Succeeded:
		for _, id := range round.Remove {
			next.Members[id] = Excluded
		}
		for _, id := range round.Add {
			next.Members[id] = Admitted
		}
		fsm.finishRound(next, &progress, now)
		next.Verifying = append(slices.Clone(round.Participants), round.Add...)
		next.VerifyFailedSince = time.Time{}
		enter(next, Serving, "round "+round.ID+" succeeded", now)
		if err := fsm.save(ctx, next); err != nil {
			return err
		}
		return fsm.verify(ctx, next, current, now)
	case engine.Failed:
		fsm.finishRound(next, &progress, now)
		if progress.Scope != engine.Joiners {
			return fsm.resetAll(ctx, next, ResetRoundFailed, progress.Reason, now)
		}
		for _, id := range round.Add {
			next.Members[id] = Rejected
		}
		if next.Phase == Recovering {
			next.Since = now
		}
		return nil
	}
	if now.Sub(next.RoundSince) > fsm.config.ApplyTimeout {
		fsm.finishRound(next, &progress, now)
		return fsm.resetAll(ctx, next, ResetApplyDeadline, "round "+round.ID+" did not finish", now)
	}
	return nil
}

func (fsm *controller) send(ctx context.Context, next *Record, current *view, plan *engine.Plan, now time.Time) error {
	if reason := checkPlan(next, current, plan); reason != "" {
		return fsm.resetAll(ctx, next, ResetUnrecoverable, reason, now)
	}
	noChange := len(plan.Remove) == 0 && len(plan.Add) == 0
	if noChange && !next.LastNoChangeRound.IsZero() && now.Sub(next.LastNoChangeRound) < fsm.config.FlapWindow {
		return fsm.resetAll(ctx, next, ResetFlap, "C6: "+plan.Reason, now)
	}
	round := engine.Round{
		ID: newID("iro-ft-", 16), Participants: slices.Clone(plan.Participants),
		Remove: slices.Clone(plan.Remove), Add: slices.Clone(plan.Add),
	}
	next.Round, next.RoundSince, next.Acceptances, next.Reason = &round, now, nil, plan.Reason
	if noChange {
		next.LastNoChangeRound = now
	}
	if inc := next.Incident; inc != nil {
		if inc.Durations.Diagnose == 0 {
			inc.Durations.Diagnose = now.Sub(inc.Opened)
		}
		affect(next, inc, round.Remove)
	}
	if err := fsm.save(ctx, next); err != nil {
		return err
	}
	acceptances, err := fsm.eng.Apply(ctx, current.obs, round)
	fsm.recorder.RoundStarted(&round, plan)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	next.Acceptances = acceptances
	if err != nil {
		fsm.finishRound(next, &engine.Progress{State: engine.Failed, Scope: engine.Group, Reason: err.Error()}, now)
		return fsm.resetAll(ctx, next, ResetRoundFailed, err.Error(), now)
	}
	return nil
}

func checkPlan(next *Record, current *view, plan *engine.Plan) string {
	seen := map[engine.MemberID]bool{}
	for _, list := range [][]engine.MemberID{plan.Participants, plan.Remove, plan.Add} {
		for _, id := range list {
			if seen[id] {
				return fmt.Sprintf("C3: member %s is named twice", id)
			}
			seen[id] = true
		}
	}
	if len(plan.Participants) == 0 {
		return "C3: no participants"
	}
	for _, id := range append(slices.Clone(plan.Participants), plan.Remove...) {
		if next.Members[id] != Admitted {
			return fmt.Sprintf("C3: member %s is not admitted", id)
		}
	}
	for _, id := range plan.Remove {
		if member := current.members[id]; member == nil || !member.Terminal {
			return fmt.Sprintf("C1: member %s is not terminal", id)
		}
	}
	for _, id := range plan.Add {
		member := current.members[id]
		if next.Members[id] != Candidate || member == nil || !member.Joinable || !joinable(next, current.group, member) {
			return fmt.Sprintf("C2: member %s cannot join", id)
		}
	}
	return ""
}

func (fsm *controller) verify(ctx context.Context, next *Record, current *view, now time.Time) error {
	// A Verify that outlives its share of the step is a failed check, not a stall.
	verifyCtx, cancel := context.WithTimeout(ctx, fsm.config.ApplyTimeout/2)
	err := fsm.eng.Verify(verifyCtx, current.obs, next.Verifying)
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		next.Verifying, next.VerifyFailedSince, next.Reason = nil, time.Time{}, "verified"
		if inc := next.Incident; inc != nil {
			inc.Durations.Verify = now.Sub(next.Since)
			fsm.closeIncident(next, outcome(inc), now)
		}
		return nil
	}
	if next.VerifyFailedSince.IsZero() && fsm.config.VerifyTimeout > 0 {
		next.VerifyFailedSince, next.Reason = now, "verify failed; retrying: "+err.Error()
		return nil
	}
	if now.Sub(next.VerifyFailedSince) < fsm.config.VerifyTimeout {
		return nil
	}
	return fsm.resetAll(ctx, next, ResetVerifyFailed, err.Error(), now)
}

func (fsm *controller) resetAll(ctx context.Context, next *Record, reason ResetReason, detail string, now time.Time) error {
	if next.Round != nil {
		fsm.finishRound(next, &engine.Progress{State: engine.Running, Reason: "dropped by reset"}, now)
	}
	for uid, pod := range next.Pods {
		pod.Removing = true
		next.Pods[uid] = pod
	}
	next.Members, next.Verifying, next.VerifyFailedSince, next.LastNoChangeRound = nil, nil, time.Time{}, time.Time{}
	if inc := next.Incident; inc != nil && inc.Durations.Diagnose == 0 {
		inc.Durations.Diagnose = now.Sub(inc.Opened)
	}
	fsm.closeIncident(next, Reset, now)
	enter(next, Starting, string(reason)+": "+detail, now)
	if err := fsm.save(ctx, next); err != nil {
		return err
	}
	pods := refs(next)
	fsm.called(fsm.wl.ResetAll(ctx, pods))
	fsm.recorder.ResetAllRequested(reason, pods)
	return nil
}

func (fsm *controller) finishRound(next *Record, progress *engine.Progress, now time.Time) {
	round, result := *next.Round, *progress
	if inc := next.Incident; inc != nil {
		inc.Rounds = append(inc.Rounds, RoundResult{Round: round, Progress: result})
		inc.Durations.Apply += now.Sub(next.RoundSince)
	}
	next.Round, next.RoundSince, next.Acceptances = nil, time.Time{}, nil
	fsm.later(func() { fsm.recorder.RoundFinished(&round, result) })
}
