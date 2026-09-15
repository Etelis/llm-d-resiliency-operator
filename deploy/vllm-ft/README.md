# Enable vLLM fault handling

This example targets one `wide-ep` LWS in `default`, with two Pods and two original
DP ranks per Pod. Adapt the namespace, workload name, model and validated minimum
survivor count in `operator.yaml` and `access.yaml`.

1. Build the operator image and set its reference in the overlay.
2. Create the `iro-vllm-ft` Secret with a random `token` key. Share it with the
   vLLM containers as `IRO_VERIFICATION_TOKEN`.
3. Install the [runtime bridge](../../runtime/vllm/README.md) in the vLLM image.
   Set `IRO_CONTROLLER_URL=http://iro-vllm-ft.default.svc:8090` and configure the
   supervisor probes and per-rank EPP filters described there.
4. Review `kubectl kustomize deploy/vllm-ft`, then apply the overlay.

The operator requires LWS, its RecoveryRequest CRD, and a vLLM runtime with the
FT capabilities listed in the design. The overlay includes the RecoveryRequest
CRD and existing operator resources. Engine fault handling is disabled without
`--enable-vllm-ft`; enabling it also requires `--leader-elect`.

Only the elected operator is ready for admission traffic. `/state` exposes the
current operation; `/admission?rank=N&pod_uid=UID` returns the rank's gate.
Round state is stored in `wide-ep-ft-state`. Keep state across operator restarts;
deleting it can lose exclusions or an in-flight operation.

FT is scoped to one ordinal LWS group with `RecreateGroupOnPodRestart` and TP=1.
The minimum survivor count must come from the deployment's expert-capacity
validation. This example does not establish that capacity for a particular model.
