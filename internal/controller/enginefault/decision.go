// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0
package enginefault

import (
	"slices"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
)

type Decision struct {
	Action       string `json:"action"`
	Participants []int  `json:"participants,omitempty"`
	Removed      []int  `json:"removed,omitempty"`
	Reason       string `json:"reason"`
}

// Decide uses engine diagnosis, not metric zero or an unreachable HTTP endpoint,
// as removal evidence. Mask-based removal requires every surviving rank's vote.
// This first deployment validates TP=1, so mask coordinates equal DP coordinates.
func Decide(allStatuses []engine.RankStatus, excluded []int, minSurvivors int) Decision {
	active := make([]*engine.RankStatus, 0, len(allStatuses))
	for index := range allStatuses {
		status := &allStatuses[index]
		if !slices.Contains(excluded, status.Rank) {
			active = append(active, status)
		}
	}
	if len(active) == 0 {
		return Decision{Action: "reset", Reason: "no active ranks"}
	}
	dead := []int{}
	fault := false
	allUnhealthy := true
	allHealthy := true
	for _, status := range active {
		if status.Status == "dead" {
			dead = append(dead, status.Rank)
		}
		if status.Status == "dead" || status.Status == "diagnosing" || status.Status == "unhealthy" {
			fault = true
		}
		allUnhealthy = allUnhealthy && status.Status == "unhealthy"
		allHealthy = allHealthy && status.Status == "healthy"
		if status.FTState == "failed" {
			return Decision{Action: "reset", Reason: "vLLM reports failed FT: " + status.FTError}
		}
	}
	if allHealthy {
		return Decision{Action: "healthy", Reason: "all active ranks healthy"}
	}
	if !fault {
		// Losing an API is not necessarily losing its EngineCore/EP worker.
		// Verify inference on healthy peers before admitting fewer frontends.
		participants := []int{}
		for _, status := range active {
			if status.Status == "healthy" {
				participants = append(participants, status.Rank)
			}
		}
		if len(participants) > 0 {
			return Decision{
				Action: "frontend_degraded", Participants: participants,
				Reason: "API unavailable; verify peer inference without changing EP membership",
			}
		}
		return Decision{Action: "wait", Reason: "unknown frontend state; no removal evidence"}
	}
	if len(dead) == 0 && allUnhealthy {
		participants := []int{}
		for _, status := range active {
			participants = append(participants, status.Rank)
		}
		return Decision{Action: "retry", Participants: participants, Reason: "all participants faulted and no dead rank"}
	}
	removed := slices.Clone(dead)
	for _, candidate := range active {
		if slices.Contains(removed, candidate.Rank) || candidate.Status == "unhealthy" || candidate.Status == "diagnosing" {
			continue
		}
		voters := 0
		unanimous := true
		for _, status := range active {
			if status.Rank == candidate.Rank || slices.Contains(dead, status.Rank) {
				continue
			}
			if status.Status != "unhealthy" || len(status.Mask) != len(allStatuses) ||
				candidate.Rank < 0 || candidate.Rank >= len(status.Mask) || status.Mask[candidate.Rank] != 1 {
				unanimous = false
				break
			}
			voters++
		}
		if unanimous && voters > 0 {
			removed = append(removed, candidate.Rank)
		}
	}
	if len(removed) == 0 {
		return Decision{Action: "wait", Reason: "waiting for engine diagnosis"}
	}
	slices.Sort(removed)
	participants := []int{}
	for _, status := range active {
		if slices.Contains(removed, status.Rank) {
			continue
		}
		participants = append(participants, status.Rank)
	}
	if len(participants) < minSurvivors {
		return Decision{Action: "reset", Reason: "insufficient configured survivor expert capacity"}
	}
	for _, status := range active {
		if !slices.Contains(removed, status.Rank) && status.Status != "unhealthy" {
			return Decision{Action: "wait", Reason: "survivors are not all unhealthy yet"}
		}
	}
	return Decision{
		Action: "scale_down", Participants: participants, Removed: removed,
		Reason: "dead rank or unanimous survivor mask evidence",
	}
}
