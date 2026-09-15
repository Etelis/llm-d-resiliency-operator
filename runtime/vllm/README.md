# vLLM runtime bridge

Mount this directory in the vLLM container and install `requirements.txt` in its Python environment. The runtime must provide fault-tolerant multi-port serving, original DP rank IDs, native `vllm:engine_healthy`, and a supervisor that preserves surviving ranks during diagnosis and FT. This bridge currently supports TP=1.

`launch.sh MODEL [vLLM options]` starts the process observer and executes vLLM. Supply the model's communication backend, expert redundancy, and FT timeout as vLLM options. Configure:

| Variable | Value |
| --- | --- |
| `IRO_CONTROLLER_URL` | IRO service URL |
| `IRO_VERIFICATION_TOKEN` | Shared Secret used only for completion verification |
| `POD_UID` | Downward API `metadata.uid` |
| `IRO_RANK_START` | First original global DP rank in this Pod |
| `IRO_LOCAL_RANKS` | Number of local ranks, at least 2 |
| `IRO_DP_SIZE` | Original group size |
| `IRO_DP_ADDRESS` | Initial DP coordinator address |
| `IRO_OBSERVER_PORT` | Diagnostic port; default 9257 |
| `IRO_SUPERVISOR_PORT` | Supervisor port; default 9256 |

For LWS, compute the start rank as worker index × local rank count in the workload launcher. Keep it unchanged after exclusion. Use supervisor `/health` and `/ready` for container probes, with LWS `RecreateGroupOnPodRestart`.

`rank_observer.py --start-rank N --local-ranks N` exposes `/status`. It retains the first observed API, EngineCore, and worker PID/creation time. Confirmed EngineCore/worker exit or replacement marks the rank dead; API-only exit and unreadable processes do not. Run it in the engine's PID namespace and keep the diagnostic port internal. Evidence is scoped to the Pod UID.

`iro_admission.AdmissionMiddleware` checks IRO admission for every inference request. Controller errors deny admission. An excluded endpoint stays closed even when its native health remains healthy; native metrics and control endpoints remain available. Only authenticated `POST /v1/completions` probes bypass the gate. Protect the shared Secret and the internal control endpoints.

`epp-config.yaml` shows the llm-d v0.10.0 filters for both native health and IRO admission. Enable experimental plugins and include every per-rank API port in the InferencePool. Missing metrics and empty candidate sets fail closed.

Run tests with `python3 -m pytest runtime/vllm` after installing `requirements-test.txt`.
