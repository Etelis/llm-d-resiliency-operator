// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

package enginefault

import (
	"flag"
	"fmt"
	"sort"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/llm-d-incubation/llm-d-resiliency-operator/internal/engine"
	"github.com/llm-d-incubation/llm-d-resiliency-operator/internal/workload/lws"
)

type Options struct {
	Enabled    bool
	Engine     string
	Workload   lws.Config
	Controller Config
}

func (options *Options) BindFlags(flags *flag.FlagSet) {
	options.Controller = Defaults()
	flags.BoolVar(&options.Enabled, "enable-engine-ft", false, "enable engine fault handling")
	flags.StringVar(&options.Engine, "engine", "", "inference engine adapter")
	flags.StringVar(&options.Workload.Namespace, "lws-namespace", "default", "LWS namespace")
	flags.StringVar(&options.Workload.Name, "lws-name", "", "LWS name")
	flags.IntVar(&options.Workload.BasePort, "engine-api-port", 8000, "first per-rank API port")
	flags.IntVar(&options.Workload.ObserverPort, "engine-observer-port", 9257, "process observer port")
	flags.IntVar(&options.Controller.StorePort, "ft-store-port", 29600, "replacement FT store port")
	flags.DurationVar(&options.Controller.DiagnosisTimeout, "ft-diagnosis-timeout",
		options.Controller.DiagnosisTimeout, "engine diagnosis deadline")
	flags.DurationVar(&options.Controller.ApplyTimeout, "ft-apply-timeout",
		options.Controller.ApplyTimeout, "FT completion deadline")
}

// Setup registers the fault handler with the adapter selected by --engine.
func (options *Options) Setup(mgr ctrl.Manager, adapters map[string]engine.Adapter) error {
	if !options.Enabled {
		return nil
	}
	adapter, ok := adapters[options.Engine]
	if !ok {
		names := make([]string, 0, len(adapters))
		for name := range adapters {
			names = append(names, name)
		}
		sort.Strings(names)
		return fmt.Errorf("unknown engine %q; available: %v", options.Engine, names)
	}
	if options.Controller.DiagnosisTimeout <= 0 || options.Controller.ApplyTimeout <= 0 ||
		options.Controller.StorePort < 1 || options.Controller.StorePort > 65535 {
		return fmt.Errorf("invalid FT deadline or store port")
	}
	workload, err := lws.New(mgr.GetAPIReader(), mgr.GetClient(), options.Workload)
	if err != nil {
		return err
	}
	runner := &Runner{
		Adapter: adapter, Workload: workload,
		Config: options.Controller,
	}
	return mgr.Add(runner)
}
