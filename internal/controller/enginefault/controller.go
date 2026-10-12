// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package enginefault defines the controller that recovers one engine group from engine
// faults. See docs/design/contracts/controller.md.
package enginefault

import (
	"context"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
)

// Controller runs one loop per engine group on the elected leader.
type Controller interface {
	// Step runs one reconciliation: read the workload and the engine, decide, act.
	// An error never changes the saved phase.
	Step(ctx context.Context) error
	// Status returns a copy of the current record.
	Status() Record
}

// Config holds the controller's settings.
type Config struct {
	PollInterval time.Duration
	// DiagnosisTimeout is the longest time in recovering without a round.
	DiagnosisTimeout time.Duration
	// ApplyTimeout is the longest time a round may stay in flight.
	ApplyTimeout time.Duration
	// VerifyTimeout is how long a failed inference probe is retried before a reset.
	VerifyTimeout time.Duration
	// FlapWindow is how long a round that changes no membership cannot repeat.
	FlapWindow time.Duration
	// CapacityRestore says how lost capacity comes back.
	CapacityRestore CapacityRestore
	// CapacityResetDelay is how long to serve below target before resetting to restore it.
	CapacityResetDelay time.Duration
	// JoinBudget is how many failed joins a group allows before it stops adding members.
	JoinBudget int
}

// Defaults returns the default settings.
func Defaults() Config {
	return Config{
		PollInterval:     time.Second,
		DiagnosisTimeout: 75 * time.Second,
		ApplyTimeout:     90 * time.Second,
		VerifyTimeout:    2 * time.Minute,
		FlapWindow:       2 * time.Minute,
		CapacityRestore:  InPlaceOrReset,
		JoinBudget:       3,
	}
}

// CapacityRestore says how lost capacity comes back.
type CapacityRestore string

// Capacity restore modes.
const (
	// InPlaceOrReset resizes in place when possible, and otherwise resets the group.
	InPlaceOrReset CapacityRestore = "in-place-or-reset"
	// InPlace only resizes in place, and otherwise stays below target.
	InPlace CapacityRestore = "in-place"
	// Off never restores capacity.
	Off CapacityRestore = "off"
)

// Phase is where the group is. No phase is terminal.
type Phase string

// Phases.
const (
	Starting   Phase = "starting"
	Serving    Phase = "serving"
	Recovering Phase = "recovering"
	Resetting  Phase = "resetting"
)

// MemberState is what the controller knows about one member.
type MemberState string

// Member states.
const (
	Candidate MemberState = "candidate"
	Admitted  MemberState = "admitted"
	Excluded  MemberState = "excluded"
	Rejected  MemberState = "rejected"
)

// PodRecord is what the controller remembers about one Pod.
type PodRecord struct {
	Name     string `json:"name"`
	Identity string `json:"identity"`
	// Removing is true once the Pod was asked to go through Resize.
	Removing bool `json:"removing,omitempty"`
}

// CapacityState is where the capacity loop is.
type CapacityState string

// Capacity states.
const (
	Full         CapacityState = "full"
	Resizing     CapacityState = "resizing"
	Waiting      CapacityState = "waiting"
	Admitting    CapacityState = "admitting"
	ResetPending CapacityState = "reset_pending"
	BelowTarget  CapacityState = "below_target"
)

// Capacity is the capacity loop's state.
type Capacity struct {
	State CapacityState `json:"state"`
	// Target is the Pod count when the group first served.
	Target int `json:"target"`
	// BelowSince is when the group went below target.
	BelowSince   time.Time `json:"below_since,omitzero"`
	JoinFailures int       `json:"join_failures,omitempty"`
}

// Record is the controller's whole state. It is what the journal stores.
type Record struct {
	Phase  Phase     `json:"phase"`
	Since  time.Time `json:"since"`
	Reason string    `json:"reason"`

	// Pods holds the group's Pods by UID. It is also what Reset names.
	Pods    map[string]PodRecord            `json:"pods"`
	Members map[engine.MemberID]MemberState `json:"members"`

	// Round is the round in flight, nil when none.
	Round       *engine.Round       `json:"round,omitempty"`
	RoundSince  time.Time           `json:"round_since,omitzero"`
	Acceptances []engine.Acceptance `json:"acceptances,omitempty"`
	Verified    []engine.MemberID   `json:"verified,omitempty"`

	LastNoChangeRound         time.Time `json:"last_no_change_round,omitzero"`
	VerifyFailedSince         time.Time `json:"verify_failed_since,omitzero"`
	DiscoveryUnavailableSince time.Time `json:"discovery_unavailable_since,omitzero"`
	RecreateStopped           bool      `json:"recreate_stopped,omitempty"`

	// Incident is the open incident, nil when none.
	Incident *Incident `json:"incident,omitempty"`
	Capacity Capacity  `json:"capacity"`
}
