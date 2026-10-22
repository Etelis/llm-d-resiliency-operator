// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

//nolint:testpackage // Documentation examples use the driver's test transport.
package vllm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/llm-d/llm-d-resiliency-operator/internal/engine"
	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

type exampleMember struct {
	State      string
	Membership string
	Status     string
	Health     int
	FTState    string `json:"ft_state"`
	RequestID  string `json:"last_ft_request_id"`
}

type examplePod struct {
	Name    string
	UID     string
	Gone    bool
	Members map[string]exampleMember
}

type exampleCall struct {
	Call     string
	In       string
	Out      string
	Requests []exampleCall
}

type exampleStep struct {
	Title string
	Phase string
	Pods  map[string]map[string]json.RawMessage
	Calls []exampleCall
}

func TestDocumentationScenarios(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("../../../docs/scenarios/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("scenario files: %v, %v", paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			var example struct {
				Engines map[string]struct {
					Pods  []examplePod
					Steps []exampleStep
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &example); err != nil {
				t.Fatal(err)
			}
			body, ok := example.Engines["vllm"]
			if !ok {
				return
			}
			test := newFleet(t, len(body.Pods), 1)
			var reconfig engine.Reconfig
			for _, step := range body.Steps {
				t.Run(step.Title, func(t *testing.T) {
					updateExample(t, body.Pods, step.Pods)
					obs, admitted := observeExample(t, test, body.Pods)
					for _, call := range step.Calls {
						checkExampleCall(t, test, &obs, admitted, &reconfig, &call, step.Phase)
					}
				})
			}
		})
	}
}

func updateExample(t *testing.T, pods []examplePod, changes map[string]map[string]json.RawMessage) {
	t.Helper()
	for index := range pods {
		pod := &pods[index]
		for key, value := range changes[pod.Name] {
			var target any
			switch key {
			case "uid":
				target = &pod.UID
				pod.Gone = false
			case "gone":
				target = &pod.Gone
			default:
				var member exampleMember
				if err := json.Unmarshal(value, &member); err != nil {
					t.Fatal(err)
				}
				pod.Members[key] = member
				continue
			}
			if err := json.Unmarshal(value, target); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func observeExample(t *testing.T, test *fleet, pods []examplePod) (engine.Observation, []engine.MemberID) {
	t.Helper()
	var present []workload.Pod
	var admitted []engine.MemberID
	for index, pod := range pods {
		member := pod.Members[fmt.Sprintf("dp%d", index)]
		fake := test.ranks[index]
		fake.set(member.Status, map[string]any{"ft_state": member.FTState, "last_ft_request_id": member.RequestID})
		fake.health, fake.down = member.Health, member.Status == "unread" || member.Status == ""
		if !pod.Gone && member.State != "starting" {
			present = append(present, test.pods[index])
		}
		if member.Membership == "admitted" {
			admitted = append(admitted, four[index])
		}
	}
	obs := test.eng.Observe(t.Context(), present)
	for index, pod := range pods {
		if pod.Gone {
			obs.Members = append(obs.Members, lost(&test.pods[index], fmt.Sprintf("dp%d", index)))
		}
	}
	for _, actual := range obs.Members {
		index := slices.Index(four, actual.ID())
		want := pods[index].Members[actual.Name].State
		if actual.Serving != (want == "serving") || actual.Terminal != (want == "dead") || actual.Unknown != (want == "unknown") {
			t.Errorf("%s: displayed %s, driver returned Serving=%t Terminal=%t Unknown=%t", actual.Name, want,
				actual.Serving, actual.Terminal, actual.Unknown)
		}
	}
	return obs, admitted
}

func checkExampleCall(t *testing.T, test *fleet, obs *engine.Observation, admitted []engine.MemberID,
	reconfig *engine.Reconfig, call *exampleCall, phase string,
) {
	t.Helper()
	switch {
	case strings.HasPrefix(call.Call, "Plan") && phase != "starting":
		plan := test.eng.Plan(*obs, admitted, nil)
		if !strings.HasPrefix(call.Out, string(plan.Kind)) {
			t.Errorf("displayed Plan %q, driver policy returned %s: %s", call.Out, plan.Kind, plan.Reason)
		}
	case strings.HasPrefix(call.Call, "Apply"):
		request := call.Requests[0]
		targets, payload, ok := strings.Cut(request.In, "; ")
		if !ok {
			t.Fatal("apply request has no payload")
		}
		var instruction struct {
			ID     string `json:"request_id"`
			Params struct {
				Removed []int `json:"removed_dp_ranks"`
			}
		}
		if err := json.Unmarshal([]byte(payload), &instruction); err != nil {
			t.Fatal(err)
		}
		*reconfig = engine.Reconfig{ID: instruction.ID}
		for _, target := range strings.Split(strings.TrimPrefix(targets, "to "), ", ") {
			for index, id := range four {
				if target == fmt.Sprintf("dp%d", index) {
					reconfig.Participants = append(reconfig.Participants, id)
				}
			}
		}
		for _, index := range instruction.Params.Removed {
			reconfig.Remove = append(reconfig.Remove, four[index])
		}
		plan := test.eng.Plan(*obs, admitted, nil)
		if !slices.Equal(plan.Participants, reconfig.Participants) || !slices.Equal(plan.Remove, reconfig.Remove) {
			t.Errorf("displayed reconfig %+v differs from policy plan %+v", reconfig, plan)
		}
		if _, err := test.eng.Apply(t.Context(), *obs, *reconfig); err != nil {
			t.Errorf("displayed accepted Apply: %v", err)
		}
	case strings.HasPrefix(call.Call, "Progress"):
		if reconfig.ID == "" {
			*reconfig = engine.Reconfig{ID: "example", Participants: admitted}
		}
		progress := test.eng.Progress(*obs, *reconfig)
		if !strings.HasPrefix(call.Out, string(progress.State)) {
			t.Errorf("displayed Progress %q, policy returned %s: %s", call.Out, progress.State, progress.Reason)
		}
	}
}
