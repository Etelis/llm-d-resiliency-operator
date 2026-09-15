// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0
package enginefault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
)

type Workload interface {
	Discover(context.Context) (engine.Group, error)
	Reset(context.Context, engine.Group) error
	Load(context.Context) ([]byte, error)
	Save(context.Context, []byte) error
}

type Config struct {
	PollInterval         time.Duration
	DiagnosisTimeout     time.Duration
	ApplyTimeout         time.Duration
	StartupTimeout       time.Duration
	RetryStabilityWindow time.Duration
	MinSurvivors         int
	StorePort            int
}

func Defaults() Config {
	return Config{
		PollInterval: time.Second, DiagnosisTimeout: 75 * time.Second, ApplyTimeout: 90 * time.Second,
		StartupTimeout: 10 * time.Minute, RetryStabilityWindow: 2 * time.Minute, MinSurvivors: 2, StorePort: 29600,
	}
}

type State struct {
	Phase                     string              `json:"phase"`
	Group                     engine.Group        `json:"group"`
	PreviousGroup             engine.Group        `json:"previous_group"`
	Excluded                  []int               `json:"excluded"`
	Admitted                  []int               `json:"admitted"`
	Status                    []engine.RankStatus `json:"status"`
	Round                     engine.Round        `json:"round"`
	Instruction               string              `json:"instruction"`
	Acceptances               []engine.Acceptance `json:"acceptances,omitempty"`
	Since                     time.Time           `json:"since"`
	DiscoveryUnavailableSince time.Time           `json:"discovery_unavailable_since,omitempty"`
	LastRetry                 time.Time           `json:"last_retry"`
	Resets                    []time.Time         `json:"resets"`
	Reason                    string              `json:"reason"`
	Sequence                  int                 `json:"sequence"`
}

type Controller struct {
	Adapter  engine.Adapter
	Workload Workload
	Config   Config
	Log      io.Writer
	mu       sync.RWMutex
	state    State
}

func New(ctx context.Context, adapter engine.Adapter, workload Workload, cfg Config, writer io.Writer) (*Controller, error) {
	controller := &Controller{
		Adapter: adapter, Workload: workload, Config: cfg, Log: writer,
		state: State{Phase: "starting", Since: time.Now().UTC()},
	}
	b, err := workload.Load(ctx)
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &controller.state); err != nil {
			return nil, fmt.Errorf("invalid saved state: %w", err)
		}
	}
	// A process replacement must revalidate before authorizing traffic.
	controller.state.Admitted = nil
	return controller, nil
}

func (controller *Controller) Snapshot() State {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	state := controller.state
	state.Group.Endpoints = slices.Clone(state.Group.Endpoints)
	state.PreviousGroup.Endpoints = slices.Clone(state.PreviousGroup.Endpoints)
	state.Excluded = slices.Clone(state.Excluded)
	state.Admitted = slices.Clone(state.Admitted)
	state.Status = slices.Clone(state.Status)
	for i := range state.Status {
		state.Status[i].Mask = slices.Clone(state.Status[i].Mask)
	}
	state.Round.Participants = slices.Clone(state.Round.Participants)
	state.Round.Removed = slices.Clone(state.Round.Removed)
	state.Acceptances = slices.Clone(state.Acceptances)
	state.Resets = slices.Clone(state.Resets)
	return state
}

//nolint:gocritic // Publish an isolated snapshot.
func (controller *Controller) set(state State) {
	controller.mu.Lock()
	controller.state = state
	controller.mu.Unlock()
}

//nolint:gocritic // Persist an isolated snapshot.
func (controller *Controller) persist(ctx context.Context, state State) error {
	state.Sequence = controller.Snapshot().Sequence + 1
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := controller.Workload.Save(ctx, b); err != nil {
		state.Admitted = nil
		controller.set(state)
		return fmt.Errorf("state persistence failed; admission closed: %w", err)
	}
	controller.set(state)
	if controller.Log != nil {
		if _, err := fmt.Fprintln(controller.Log, string(b)); err != nil {
			return fmt.Errorf("write transition log: %w", err)
		}
	}
	return nil
}

func allHealthy(status []engine.RankStatus, ranks []int) bool {
	if len(ranks) == 0 {
		return false
	}
	for _, rank := range ranks {
		i := slices.IndexFunc(status, func(state engine.RankStatus) bool { return state.Rank == rank })
		if i < 0 || status[i].Status != "healthy" || status[i].FTState == "failed" {
			return false
		}
	}
	return true
}

//nolint:gocritic // Read an isolated snapshot.
func activeRanks(state State) []int {
	r := []int{}
	for _, e := range state.Group.Endpoints {
		if !slices.Contains(state.Excluded, e.Rank) {
			r = append(r, e.Rank)
		}
	}
	return r
}

func overlaps(a, b engine.Group) bool {
	for _, x := range a.Endpoints {
		for _, y := range b.Endpoints {
			if x.PodUID == y.PodUID {
				return true
			}
		}
	}
	return false
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "iro-ft-" + hex.EncodeToString(b[:])
}

//nolint:gocritic // Save the transition before reset.
func (controller *Controller) beginReset(ctx context.Context, state State, reason string) error {
	now := time.Now().UTC()
	state.Admitted = nil
	controller.set(state)
	state.Resets = slices.DeleteFunc(state.Resets, func(t time.Time) bool { return now.Sub(t) > 30*time.Minute })
	if len(state.Resets) >= 3 {
		state.Phase = "intervention_required"
		state.Reason = "reset budget exhausted: " + reason
		state.Since = now
		return controller.persist(ctx, state)
	}
	state.Resets = append(state.Resets, now)
	state.Phase = "resetting"
	state.Reason = reason
	state.Since = now
	state.PreviousGroup = state.Group
	if err := controller.persist(ctx, state); err != nil {
		return err
	}
	return controller.Workload.Reset(ctx, state.PreviousGroup)
}

//nolint:gocritic // Verification must not mutate concurrently published controller state.
func (controller *Controller) verify(ctx context.Context, state State, ranks []int, allowDiagnosis bool) error {
	state.Admitted = nil
	controller.set(state)
	err := controller.Adapter.Verify(ctx, state.Group, ranks)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		if allowDiagnosis {
			return controller.waitForDiagnosis(ctx, state, "frontend verification failed: "+err.Error())
		}
		return controller.beginReset(ctx, state, "inference verification failed: "+err.Error())
	}
	// Verify against current workload identities as well as engine outputs.
	group, err := controller.Workload.Discover(ctx)
	if err != nil {
		return controller.discoveryUnavailable(ctx, state, err)
	}
	if group.ID != state.Group.ID {
		state.Phase = "starting"
		state.Since = time.Now().UTC()
		state.Reason = "workload changed during verification"
		return controller.persist(ctx, state)
	}
	state.Status = controller.Adapter.EngineStatus(ctx, group)
	if !allHealthy(state.Status, ranks) {
		if allowDiagnosis {
			return controller.waitForDiagnosis(ctx, state, "engine health changed during frontend verification")
		}
		return controller.beginReset(ctx, state, "engine health changed during verification")
	}
	state.Phase = "serving"
	state.Since = time.Now().UTC()
	state.DiscoveryUnavailableSince = time.Time{}
	state.Admitted = slices.Clone(ranks)
	state.Reason = "inference verified"
	return controller.persist(ctx, state)
}

//nolint:gocritic // Preserve the original diagnosis deadline across repeated verification failures.
func (controller *Controller) waitForDiagnosis(ctx context.Context, state State, reason string) error {
	now := time.Now().UTC()
	if state.Phase != "diagnosing" {
		state.Phase = "diagnosing"
		state.Since = now
	}
	if now.Sub(state.Since) > controller.Config.DiagnosisTimeout {
		return controller.beginReset(ctx, state, "diagnosis deadline exceeded: "+reason)
	}
	state.Reason = reason
	return controller.persist(ctx, state)
}

//nolint:gocritic // Discovery uncertainty must preserve the previous reconciliation snapshot.
func (controller *Controller) discoveryUnavailable(ctx context.Context, state State, err error) error {
	now := time.Now().UTC()
	state.Admitted = nil
	if state.DiscoveryUnavailableSince.IsZero() {
		state.DiscoveryUnavailableSince = now
	}
	state.Reason = "workload discovery unavailable: " + err.Error()
	if now.Sub(state.DiscoveryUnavailableSince) > controller.Config.StartupTimeout {
		state.Phase = "intervention_required"
		state.Reason = "workload discovery deadline exceeded; workload identity remains unconfirmed: " + err.Error()
	}
	// A failed Kubernetes read does not prove a Pod/container replacement. Keep
	// the operation and its original deadline so an unchanged group resumes
	// reconciliation after discovery succeeds. Never reset an unconfirmed group.
	controller.set(state)
	return controller.persist(ctx, state)
}

// Step performs one reconciliation. Callers must serialize it per group.
func (controller *Controller) Step(ctx context.Context) error {
	state := controller.Snapshot()
	now := time.Now().UTC()
	group, err := controller.Workload.Discover(ctx)
	if state.Phase == "intervention_required" {
		// Keep reporting the failure, but allow an externally repaired/new
		// deployment to return to service without another destructive action.
		if err != nil || (state.Group.ID != "" && (group.ID == state.Group.ID || overlaps(group, state.Group))) {
			return err
		}
		state.Group = group
		state.Excluded = nil
		state.Round = engine.Round{}
		state.Instruction = ""
		state.Status = controller.Adapter.EngineStatus(ctx, group)
		if allHealthy(state.Status, activeRanks(state)) {
			return controller.verify(ctx, state, activeRanks(state), false)
		}
		return nil
	}
	if err != nil {
		return controller.discoveryUnavailable(ctx, state, err)
	}
	if state.Phase == "resetting" {
		if overlaps(group, state.PreviousGroup) {
			if now.Sub(state.Since) > controller.Config.StartupTimeout {
				state.Phase = "intervention_required"
				state.Reason = "old group not fully replaced"
				return controller.persist(ctx, state)
			}
			return controller.Workload.Reset(ctx, state.PreviousGroup)
		}
		state.Group = group
		state.Phase = "starting"
		state.Excluded = nil
		state.Round = engine.Round{}
		state.Instruction = ""
		state.Acceptances = nil
		state.LastRetry = time.Time{}
		state.DiscoveryUnavailableSince = time.Time{}
		state.Reason = "replacement group discovered"
		if err := controller.persist(ctx, state); err != nil {
			return err
		}
	}
	if group.ID != state.Group.ID && state.Group.ID != "" && overlaps(group, state.Group) {
		if state.Phase != "starting" {
			state.Phase = "starting"
			state.Since = now
			state.PreviousGroup = state.Group
			state.Reason = "waiting for complete LWS group replacement"
			return controller.persist(ctx, state)
		}
		if now.Sub(state.Since) > controller.Config.StartupTimeout {
			state.Phase = "intervention_required"
			state.Reason = "partial LWS replacement did not complete"
			return controller.persist(ctx, state)
		}
		return nil
	}
	if group.ID != state.Group.ID {
		state.Admitted = nil
		controller.set(state)

		state.Group = group
		state.Excluded = nil
		state.Round = engine.Round{}
		state.Instruction = ""
		state.Acceptances = nil
		state.LastRetry = time.Time{}
		state.DiscoveryUnavailableSince = time.Time{}
		state.Phase = "starting"
		state.Since = now
		state.Reason = "new group discovered"
		if err := controller.persist(ctx, state); err != nil {
			return err
		}
	}
	state.Status = controller.Adapter.EngineStatus(ctx, group)
	controller.set(state)
	active := activeRanks(state)
	if state.Phase == "starting" {
		if allHealthy(state.Status, active) {
			return controller.verify(ctx, state, active, false)
		}
		if now.Sub(state.Since) > controller.Config.StartupTimeout {
			state.Phase = "intervention_required"
			state.Reason = "group failed to start healthy"
			return controller.persist(ctx, state)
		}
		return nil
	}
	if state.Phase == "applying" {
		return controller.finishRound(ctx, state, now)
	}

	decision := Decide(state.Status, state.Excluded, controller.Config.MinSurvivors)
	if decision.Action == "frontend_degraded" {
		if state.Phase == "serving" && slices.Equal(state.Admitted, decision.Participants) {
			return nil
		}
		return controller.verify(ctx, state, decision.Participants, true)
	}
	if decision.Action == "healthy" {
		if state.Phase != "serving" || len(state.Admitted) != len(active) {
			return controller.verify(ctx, state, active, false)
		}
		return nil
	}
	// Close admission before any persistent state write or FT request.
	state.Admitted = nil
	controller.set(state)
	if state.Phase != "diagnosing" {
		state.Phase = "diagnosing"
		state.Since = now
		state.Reason = decision.Reason
		if err := controller.persist(ctx, state); err != nil {
			return err
		}
		if decision.Action == "wait" {
			return controller.probeDiagnosis(ctx, state)
		}
		return nil
	}
	if decision.Action == "reset" {
		return controller.beginReset(ctx, state, decision.Reason)
	}
	if now.Sub(state.Since) > controller.Config.DiagnosisTimeout {
		return controller.beginReset(ctx, state, "diagnosis deadline exceeded: "+decision.Reason)
	}
	if decision.Action == "wait" || now.Sub(state.Since) < 2*controller.Config.PollInterval {
		return nil
	}
	if decision.Action == "retry" && !state.LastRetry.IsZero() && now.Sub(state.LastRetry) < controller.Config.RetryStabilityWindow {
		return controller.beginReset(ctx, state, "retry failed sustained serving within the stability window")
	}
	round := engine.Round{ID: newID(), GroupID: group.ID, Participants: decision.Participants, Removed: decision.Removed}
	if len(active) > 0 && slices.Contains(decision.Removed, slices.Min(active)) {
		first := decision.Participants[0]
		for _, e := range group.Endpoints {
			if e.Rank == first {
				u, err := url.Parse(e.URL)
				if err != nil {
					return err
				}
				round.MasterIP = u.Hostname()
			}
		}
		round.StorePort = controller.Config.StorePort
	}
	state.Round = round
	state.Instruction = decision.Action
	state.Acceptances = nil
	state.Phase = "applying"
	state.Since = now
	state.Reason = decision.Reason
	if decision.Action == "retry" {
		state.LastRetry = now
	}
	// Persist dispatch intent first. A replacement process never replays it.
	if err := controller.persist(ctx, state); err != nil {
		return err
	}
	if decision.Action == "retry" {
		state.Acceptances, err = controller.Adapter.ResumeEngine(ctx, group, round)
	} else {
		state.Acceptances, err = controller.Adapter.ScaleDown(ctx, group, round)
	}
	if err != nil {
		return controller.beginReset(ctx, state, "FT dispatch failed: "+err.Error())
	}
	return controller.persist(ctx, state)
}

//nolint:gocritic // Probe once on entry to diagnosis; preserve the bounded transition snapshot.
func (controller *Controller) probeDiagnosis(ctx context.Context, state State) error {
	var healthy []int
	for index := range state.Status {
		status := &state.Status[index]
		if status.Status == "healthy" && !slices.Contains(state.Excluded, status.Rank) {
			healthy = append(healthy, status.Rank)
		}
	}
	if len(healthy) == 0 {
		return nil
	}
	// An idle peer may not observe a dead worker until it enters a collective.
	// A bounded internal completion triggers that diagnosis while user traffic
	// remains closed. Its result never authorizes admission or proves FT success.
	err := controller.Adapter.Verify(ctx, state.Group, healthy)
	state.Reason = "waiting for engine diagnosis after a bounded inference probe"
	if err != nil {
		state.Reason += ": " + err.Error()
	}
	return controller.persist(ctx, state)
}

func (controller *Controller) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(controller.Snapshot()); err != nil {
			log.Printf("encode state: %v", err)
		}
	})
	mux.HandleFunc("GET /admission", func(w http.ResponseWriter, r *http.Request) {
		rank, err := strconv.Atoi(r.URL.Query().Get("rank"))
		uid := r.URL.Query().Get("pod_uid")
		state := controller.Snapshot()
		allowed := false
		if err == nil && uid != "" && state.Phase == "serving" && slices.Contains(state.Admitted, rank) {
			for _, e := range state.Group.Endpoints {
				if e.Rank == rank && e.PodUID == uid {
					allowed = true
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		reply := map[string]any{"admitted": allowed, "group_id": state.Group.ID, "sequence": state.Sequence}
		if err := json.NewEncoder(w).Encode(reply); err != nil {
			log.Printf("encode admission: %v", err)
		}
	})
	return mux
}

//nolint:gocritic // Each reconciliation owns a value snapshot.
func (controller *Controller) finishRound(ctx context.Context, state State, now time.Time) error {
	for index := range state.Status {
		status := &state.Status[index]
		if !slices.Contains(state.Round.Participants, status.Rank) {
			continue
		}
		if status.Status == "dead" || status.FTState == "failed" {
			return controller.beginReset(ctx, state, fmt.Sprintf("rank %d FT failed: %s", status.Rank, status.FTError))
		}
		if status.RequestID != "" && status.RequestID != state.Round.ID {
			return controller.beginReset(ctx, state, "conflicting engine FT operation")
		}
	}
	if allHealthy(state.Status, state.Round.Participants) {
		state.Excluded = append(state.Excluded, state.Round.Removed...)
		slices.Sort(state.Excluded)
		state.Excluded = slices.Compact(state.Excluded)
		return controller.verify(ctx, state, state.Round.Participants, false)
	}
	if now.Sub(state.Since) > controller.Config.ApplyTimeout {
		return controller.beginReset(ctx, state, "FT completion deadline exceeded")
	}
	return nil
}
