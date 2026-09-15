# vLLM fault handling validation

Run `make test` for the Go protocol, coordinator, lifecycle and Kubernetes-client
tests. Run `python -m pytest runtime/vllm -q` with the runtime test dependencies.
The tests cover shared commands, partial acceptance, status and identity checks,
process-exit evidence, timeouts, reset, state conflicts, restart without replay,
admission and operator shutdown.

Build, full Go race tests, 35 runtime tests, scoped Go lint, Ruff and the Kustomize
render pass. Full-repository lint still reports existing RecoveryRequest and CRD
style issues from `initialcontroller`. A container image build has not been run.

The following GPU results come from the preceding standalone coordinator fixture:
two H200 nodes, two Pods, DP/EP=4, TP=1 and DeepSeek-V2-Lite. They validate the
protocol and fault-handling approach. The new operator-manager integration still
requires cluster revalidation.

| Fault | Observed result |
| --- | --- |
| Worker rank 1 exits | Survivor FT passed; 825 sustained requests and 24/24 output checks passed. |
| Retryable execution exception | Retry passed; 822 sustained requests, 24/24 outputs and subsequent EPLB passed. |
| API rank 1 exits | Passed without an FT command or EP membership change; 907 sustained requests passed. |
| Store master rank 0 exits | Survivor FT passed; 832 sustained requests and 24/24 outputs passed. |
| Three workers exit below configured capacity | IRO requested LWS reset; both Pods replaced, 472 sustained requests and 24/24 outputs passed. |
| Container exits | LWS replaced both Pods; 466 sustained requests and 24/24 outputs passed. |
| EngineCore rank 1 exits | FT and 829 sustained requests passed; one of 24 output comparisons differed. |
| Worker rank 1 stalls | Exclusion and admission worked; one of 845 sustained requests received a gateway transport error. |
| Workers 1 and 3 exit | Backend tensor-shape failure; LWS reset restored serving. Investigation deferred. |

Pod loss, all-local API loss, idle-worker loss,
stalled-worker retest and an EngineCore quality comparison are in the remaining
fixture queue. Do not interpret queued cases or qualified results as passes.

Before enabling a deployment, verify both inference and routing after each fault.
In-place success must preserve Pod, container and surviving process identities.
Reset must replace the old group. Observe retry through later EPLB activity and
ensure excluded endpoints cannot rejoin, including after they become reachable.
In-flight requests may fail; replay and failed-process repair are outside scope.
