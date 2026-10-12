// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package workload defines how IRO reads and changes the Pods of one engine group.
// See docs/design/contracts/workload.md.
package workload

import (
	"context"
	"errors"
)

// Workload reports the Pods of one engine group as they are now, and recreates or
// resizes the group on request. It keeps no memory of earlier calls or groups.
type Workload interface {
	// Discover reports the group as it is now. It fails only when the workload cannot
	// be read or its spec is not supported.
	Discover(ctx context.Context) (Group, error)
	// Reset recreates the group these Pods belong to. It does nothing once none of
	// them exist, so it can be repeated.
	Reset(ctx context.Context, pods []PodRef) error
	// Resize makes the group hold req.Pods Pods, removing exactly req.Remove, without
	// recreating it. A workload that cannot resize returns ErrUnsupported.
	Resize(ctx context.Context, req ResizeRequest) error
}

// ErrUnsupported means the workload cannot do this at all. Nothing was changed.
var ErrUnsupported = errors.New("unsupported")

// Group is the group as it is now.
type Group struct {
	// Pods holds every Pod, sorted by Index, including starting and terminating ones.
	Pods []Pod
	// Recreating is true while the workload replaces the whole group.
	Recreating bool
	// RecreateStopped is true when the workload gave up recreating the group.
	RecreateStopped bool
}

// Pod is one Pod of the group.
type Pod struct {
	Name string
	UID  string
	IP   string
	// Index is the Pod's position in the group, fixed for its life.
	Index int
	// Identity is the UID plus container IDs and restart counts. It changes when any
	// container restarts.
	Identity string
	// Node is the node the Pod runs on, empty until scheduled.
	Node  string
	State PodState
}

// Ref names the Pod exactly.
func (pod *Pod) Ref() PodRef { return PodRef{Name: pod.Name, UID: pod.UID} }

// PodState is a Pod's lifecycle as the workload sees it.
type PodState string

// Pod states.
const (
	PodStarting    PodState = "Starting"
	PodReady       PodState = "Ready"
	PodTerminating PodState = "Terminating"
)

// PodRef names one Pod. The UID makes it exact: a new Pod with the same name is a
// different Pod.
type PodRef struct {
	Name string
	UID  string
}

// ResizeRequest is the shape the controller wants.
type ResizeRequest struct {
	// Pods is how many Pods the group should hold.
	Pods int
	// Remove lists the Pods that must go. No other Pod may be removed.
	Remove []PodRef
}
