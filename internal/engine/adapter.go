// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package engine defines fault handling across engine implementations.
package engine

import (
	"context"
	"errors"
	"time"
)

var ErrUnsupported = errors.New("engine capability is unsupported")

type Endpoint struct {
	Rank           int    `json:"rank"`
	URL            string `json:"url"`
	Pod            string `json:"pod"`
	PodUID         string `json:"pod_uid"`
	DiagnosticsURL string `json:"diagnostics_url,omitempty"`
}

type Group struct {
	ID        string     `json:"id"`
	Endpoints []Endpoint `json:"endpoints"`
}

type RankStatus struct {
	Rank      int    `json:"rank"`
	Status    string `json:"status"`
	FaultInfo string `json:"fault_info,omitempty"`
	Mask      []int  `json:"mask,omitempty"`
	FTState   string `json:"ft_state,omitempty"`
	FTError   string `json:"ft_error,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type FaultEvent struct {
	GroupID string       `json:"group_id"`
	At      time.Time    `json:"at"`
	Ranks   []RankStatus `json:"ranks"`
}

// Round identifies original DP coordinates, never a renumbered survivor set.
type Round struct {
	ID           string `json:"id"`
	GroupID      string `json:"group_id"`
	Participants []int  `json:"participants"`
	Removed      []int  `json:"removed,omitempty"`
	MasterIP     string `json:"master_ip,omitempty"`
	StorePort    int    `json:"store_port,omitempty"`
}

type Acceptance struct {
	Rank     int    `json:"rank"`
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

// Adapter exposes engine fault handling. ResumeEngine retries faulted engines.
type Adapter interface {
	EngineStatus(context.Context, Group) []RankStatus
	FaultEvents(context.Context, Group, time.Duration) <-chan FaultEvent
	ResumeEngine(context.Context, Group, Round) ([]Acceptance, error)
	ScaleDown(context.Context, Group, Round) ([]Acceptance, error)
	PauseEngine(context.Context, Group) error
	ScaleUp(context.Context, Group, int) error
	Verify(context.Context, Group, []int) error
}
