// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package engine defines what an inference engine plugin offers the controller: a
// driver that observes and acts, and a policy that decides. See docs/design/contracts/engine.md.
package engine

import (
	"context"

	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

// Engine is one inference engine plugin.
type Engine interface {
	Capabilities() Capabilities
	Driver
	Policy
}

// Capabilities says what the engine can do at all.
type Capabilities struct {
	// Grow is true when the engine can admit new members into a running group.
	Grow bool
}

// Driver does the engine's I/O. It keeps no memory: everything it needs travels in the
// Observation. Every call is bounded by a timeout.
type Driver interface {
	// Observe reports the members running on these Pods and their state. It never
	// fails: a Pod it cannot reach gives no members.
	Observe(ctx context.Context, pods []workload.Pod) Observation
	// Probe nudges the engine to notice a fault it has not seen yet. It may do nothing.
	Probe(ctx context.Context, observation Observation) error
	// Apply sends the round to its participants and joiners. It returns an error
	// unless every one accepted. The controller never sends a round twice.
	Apply(ctx context.Context, observation Observation, round Round) ([]Acceptance, error)
	// Verify sends one real, bounded inference request through each member.
	Verify(ctx context.Context, observation Observation, members []MemberID) error
}

// Policy decides. It is pure: no I/O, no clock, no globals.
type Policy interface {
	// Plan says what should happen now.
	Plan(observation Observation, admitted, candidates []MemberID) Plan
	// Progress says how the round in flight is going.
	Progress(observation Observation, round Round) Progress
}

// MemberID names one engine member in one Pod's life: "Name@PodIdentity".
type MemberID string

// Member is one engine member as the driver sees it now.
type Member struct {
	Pod workload.PodRef
	// PodIdentity is copied from the Pod, so the ID changes when the Pod restarts.
	PodIdentity string
	// Name is the engine's name for the member, unique within its Pod.
	Name string
	// Serving is the engine's native health: the router may send it traffic.
	Serving bool
	// Terminal means the member will never serve again.
	Terminal bool
	// Joinable means a candidate can be admitted now.
	Joinable bool
	// Detail is for status and logs only.
	Detail string
}

// ID is the same for anyone who computes it.
func (member *Member) ID() MemberID { return MemberID(member.Name + "@" + member.PodIdentity) }

// Observation is everything the driver saw in one Observe call.
type Observation struct {
	Members []Member
	// Engine is driver-private: addresses and raw state, for the same engine's driver
	// and policy.
	Engine any
}

// Round is one recovery or resize action. The controller builds it from a Plan.
type Round struct {
	ID           string
	Participants []MemberID
	Remove       []MemberID
	Add          []MemberID
}

// Acceptance is one member's answer to Apply. Accepted means started, not done.
type Acceptance struct {
	Member   MemberID
	Accepted bool
	Error    string
}

// Plan is the policy's answer to "what now?". Remove and Add tell a retry, shrink,
// grow and replace apart.
type Plan struct {
	Kind         PlanKind
	Participants []MemberID
	Remove       []MemberID
	Add          []MemberID
	Reason       string
}

// PlanKind is what the controller should do with a plan.
type PlanKind string

// Plan kinds.
const (
	Healthy       PlanKind = "Healthy"
	Wait          PlanKind = "Wait"
	Change        PlanKind = "Change"
	Unrecoverable PlanKind = "Unrecoverable"
)

// Progress is the policy's answer to "how is the round going?".
type Progress struct {
	State ProgressState
	// Scope is set when State is Failed.
	Scope  FailureScope
	Reason string
}

// ProgressState is where a round is.
type ProgressState string

// Progress states.
const (
	Running   ProgressState = "Running"
	Succeeded ProgressState = "Succeeded"
	Failed    ProgressState = "Failed"
)

// FailureScope says what a failed round may have broken.
type FailureScope string

// Failure scopes.
const (
	// Joiners means only the joining members failed; admitted members are untouched.
	Joiners FailureScope = "Joiners"
	// Group means the group may be broken and must be reset.
	Group FailureScope = "Group"
)
