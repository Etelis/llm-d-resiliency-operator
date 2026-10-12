// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

// Recorder turns what the controller does into logs, metrics, Events and incident
// records. It is best-effort: it never blocks, and IRO never reads it back.
// See docs/design/contracts/telemetry.md.
type Recorder interface {
	// Stepped is called at the end of every Step.
	Stepped(record *Record, err error)
	PhaseChanged(from, to Phase, reason string)
	IncidentOpened(incident *Incident)
	RoundStarted(round *engine.Round, plan *engine.Plan)
	RoundFinished(round *engine.Round, progress engine.Progress)
	ResetRequested(reason ResetReason, pods []workload.PodRef)
	ResizeRequested(req workload.ResizeRequest, err error)
	SaveFailed(err error)
	IncidentClosed(incident *Incident)
	// Ready fails when this process leads and its loop stopped making progress.
	Ready() error
}

// Incident is one fault, from the moment IRO sees it until the group serves again or
// is recreated. It opens when the engine reports a fault, or when the workload starts
// recreating a serving group.
type Incident struct {
	ID     string    `json:"id"`
	Opened time.Time `json:"opened"`
	Closed time.Time `json:"closed,omitzero"`
	// Cause is the reason of the first plan that was not Healthy.
	Cause     string            `json:"cause"`
	Affected  []engine.MemberID `json:"affected,omitempty"`
	Pods      []workload.PodRef `json:"pods,omitempty"`
	Nodes     []string          `json:"nodes,omitempty"`
	Rounds    []RoundResult     `json:"rounds,omitempty"`
	Outcome   Outcome           `json:"outcome,omitempty"`
	Durations Durations         `json:"durations"`
}

// RoundResult is one round sent for an incident, and how it ended.
type RoundResult struct {
	Round    engine.Round    `json:"round"`
	Progress engine.Progress `json:"progress"`
}

// Outcome is how an incident ended.
type Outcome string

// Outcomes.
const (
	// Recovered means serving again with the same members.
	Recovered Outcome = "Recovered"
	// Shrunk means serving again with fewer members.
	Shrunk Outcome = "Shrunk"
	// Reset means IRO asked the workload to recreate the group.
	Reset Outcome = "Reset"
	// Recreated means the workload recreated the group on its own.
	Recreated Outcome = "Recreated"
)

// Durations splits an incident into stages.
type Durations struct {
	// Diagnose runs from the incident opening to the first round, or to a reset.
	Diagnose time.Duration `json:"diagnose"`
	Apply    time.Duration `json:"apply"`
	Verify   time.Duration `json:"verify"`
	Total    time.Duration `json:"total"`
}

// ResetReason is why IRO asked for a reset. It is a fixed set, so it can be a metric label.
type ResetReason string

// Reset reasons.
const (
	ResetUnrecoverable     ResetReason = "unrecoverable"
	ResetRoundFailed       ResetReason = "round_failed"
	ResetDiagnosisDeadline ResetReason = "diagnosis_deadline"
	ResetApplyDeadline     ResetReason = "apply_deadline"
	ResetFlap              ResetReason = "flap"
	ResetVerifyFailed      ResetReason = "verify_failed"
	ResetExclusionBroken   ResetReason = "exclusion_broken"
	ResetCapacity          ResetReason = "capacity"
)
