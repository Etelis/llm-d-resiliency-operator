// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package enginefault is the controller. See docs/design/contracts/controller.md.
package enginefault

import (
	"context"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
)

type Controller interface {
	Step(ctx context.Context) error
}

type Config struct {
	PollInterval     time.Duration
	DiagnosisTimeout time.Duration
	ApplyTimeout     time.Duration
	VerifyTimeout    time.Duration
	FlapWindow       time.Duration
	CapacityRestore  CapacityRestore
}

type CapacityRestore string

const (
	RestoreResetPod CapacityRestore = "reset-pod"
	RestoreResetAll CapacityRestore = "reset-all"
	RestoreRelease  CapacityRestore = "release"
)

type Phase string

const (
	Starting   Phase = "starting"
	Serving    Phase = "serving"
	Recovering Phase = "recovering"
)

type MemberState string

const (
	Candidate MemberState = "candidate"
	Admitted  MemberState = "admitted"
	Excluded  MemberState = "excluded"
	Rejected  MemberState = "rejected"
)

type PodRecord struct {
	Name     string `json:"name"`
	Identity string `json:"identity"`
	Removing bool   `json:"removing,omitempty"`
}

// Record is what the journal stores.
type Record struct {
	Phase  Phase     `json:"phase"`
	Since  time.Time `json:"since"`
	Reason string    `json:"reason"`

	Pods    map[string]PodRecord            `json:"pods"`
	Members map[engine.MemberID]MemberState `json:"members"`

	Round       *engine.Round       `json:"round,omitempty"`
	RoundSince  time.Time           `json:"round_since,omitzero"`
	Acceptances []engine.Acceptance `json:"acceptances,omitempty"`
	Verified    []engine.MemberID   `json:"verified,omitempty"`

	LastNoChangeRound         time.Time `json:"last_no_change_round,omitzero"`
	VerifyFailedSince         time.Time `json:"verify_failed_since,omitzero"`
	DiscoveryUnavailableSince time.Time `json:"discovery_unavailable_since,omitzero"`
	RecreateStopped           bool      `json:"recreate_stopped,omitempty"`

	Incident *Incident `json:"incident,omitempty"`
}
