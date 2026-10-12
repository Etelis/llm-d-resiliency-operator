// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package lws is the LeaderWorkerSet workload. See docs/design/lws.md.
package lws

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"slices"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/llm-d/llm-d-resiliency-operator/internal/journal"
	"github.com/llm-d/llm-d-resiliency-operator/internal/journal/configmap"
	"github.com/llm-d/llm-d-resiliency-operator/internal/workload"
)

const (
	labelPrefix     = "leaderworkerset.sigs.k8s.io/"
	recreatePending = labelPrefix + "group-recreate-pending"
	budgetExhausted = labelPrefix + "group-restart-budget-exhausted"
)

type Options struct {
	Namespace string
	Name      string
}

func (options *Options) BindFlags(flags *flag.FlagSet) {
	flags.StringVar(&options.Namespace, "lws-namespace", "default", "namespace of the LeaderWorkerSet")
	flags.StringVar(&options.Name, "lws-name", "", "name of the LeaderWorkerSet")
}

func (options *Options) New(reader client.Reader, writer client.Client) (workload.Workload, journal.Journal, error) {
	if reader == nil || writer == nil || options.Namespace == "" || options.Name == "" {
		return nil, nil, errors.New("LWS workload needs --lws-namespace and --lws-name")
	}
	lws := &Workload{reader: reader, writer: writer, namespace: options.Namespace, name: options.Name}
	return lws, configmap.New(reader, writer, lws.object()), nil
}

type Workload struct {
	reader    client.Reader
	writer    client.Client
	namespace string
	name      string
}

func (lws *Workload) Discover(ctx context.Context) (workload.Group, error) {
	set := lws.object()
	if err := lws.reader.Get(ctx, client.ObjectKeyFromObject(set), set); err != nil {
		return workload.Group{}, err
	}
	recreates, err := spec(set)
	if err != nil {
		return workload.Group{}, err
	}
	var list corev1.PodList
	if err := lws.reader.List(ctx, &list, client.InNamespace(lws.namespace), client.MatchingLabels{
		labelPrefix + "name": lws.name, labelPrefix + "group-index": "0",
	}); err != nil {
		return workload.Group{}, err
	}
	var group workload.Group
	var leader *corev1.Pod
	for index := range list.Items {
		pod := &list.Items[index]
		worker, err := strconv.Atoi(pod.Labels[labelPrefix+"worker-index"])
		if err != nil || worker < 0 || pod.Name != lws.podName(worker) {
			return workload.Group{}, fmt.Errorf("invalid worker identity for Pod %s", pod.Name)
		}
		if worker == 0 {
			leader = pod
		}
		group.Pods = append(group.Pods, workload.Pod{
			Name: pod.Name, UID: string(pod.UID), IP: pod.Status.PodIP, Index: worker,
			Identity: identity(pod), Node: pod.Spec.NodeName, State: state(pod),
		})
	}
	slices.SortFunc(group.Pods, func(left, right workload.Pod) int { return cmp.Compare(left.Index, right.Index) })
	group.RecreateStopped = annotated(leader, budgetExhausted)
	group.Recreating = !group.RecreateStopped && recreating(leader, list.Items, recreates)
	return group, nil
}

func (lws *Workload) Reset(ctx context.Context, pod workload.PodRef) error {
	if pod.Name == lws.podName(0) {
		return workload.ErrUnsupported
	}
	set := lws.object()
	if err := lws.reader.Get(ctx, client.ObjectKeyFromObject(set), set); err != nil {
		return err
	}
	recreates, err := spec(set)
	if err != nil {
		return err
	}
	if recreates {
		return workload.ErrUnsupported
	}
	return lws.delete(ctx, pod)
}

func (lws *Workload) ResetAll(ctx context.Context, pods []workload.PodRef) error {
	index := slices.IndexFunc(pods, func(pod workload.PodRef) bool { return pod.Name == lws.podName(0) })
	if index < 0 {
		return errors.New("ResetAll needs the leader Pod")
	}
	return lws.delete(ctx, pods[index], client.PropagationPolicy(metav1.DeletePropagationForeground))
}

func (*Workload) Release(context.Context, []workload.PodRef) error {
	return workload.ErrUnsupported
}

func (lws *Workload) delete(ctx context.Context, ref workload.PodRef, options ...client.DeleteOption) error {
	if ref.UID == "" {
		return fmt.Errorf("pod %s has no UID", ref.Name)
	}
	uid := types.UID(ref.UID)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: lws.namespace, Name: ref.Name}}
	err := lws.writer.Delete(ctx, pod, append(options, client.Preconditions{UID: &uid})...)
	if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		return nil
	}
	return err
}

func (lws *Workload) object() *unstructured.Unstructured {
	set := &unstructured.Unstructured{}
	set.SetAPIVersion("leaderworkerset.x-k8s.io/v1")
	set.SetKind("LeaderWorkerSet")
	set.SetNamespace(lws.namespace)
	set.SetName(lws.name)
	return set
}

func (lws *Workload) podName(index int) string {
	if index == 0 {
		return lws.name + "-0"
	}
	return lws.name + "-0-" + strconv.Itoa(index)
}

// spec reports whether the LWS recreates the whole group when one Pod fails.
func spec(set *unstructured.Unstructured) (bool, error) {
	var value struct {
		Spec struct {
			Replicas             *int64 `json:"replicas"`
			GroupIdentity        string `json:"groupIdentity"`
			LeaderWorkerTemplate struct {
				Size          *int64 `json:"size"`
				RestartPolicy string `json:"restartPolicy"`
			} `json:"leaderWorkerTemplate"`
		} `json:"spec"`
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(set.Object, &value); err != nil {
		return false, err
	}
	template := value.Spec.LeaderWorkerTemplate
	switch {
	case !set.GetDeletionTimestamp().IsZero():
		return false, errors.New("LWS is being deleted")
	case value.Spec.Replicas == nil || *value.Spec.Replicas != 1:
		return false, errors.New("LWS must have exactly one replica")
	case template.Size == nil || *template.Size < 1:
		return false, errors.New("LWS group size must be positive")
	case value.Spec.GroupIdentity != "" && value.Spec.GroupIdentity != "Ordinal":
		return false, errors.New("LWS groupIdentity must be Ordinal")
	}
	switch template.RestartPolicy {
	case "", "RecreateGroupOnPodRestart":
		return true, nil
	case "None":
		return false, nil
	}
	return false, fmt.Errorf("unsupported LWS restartPolicy %q", template.RestartPolicy)
}

func recreating(leader *corev1.Pod, pods []corev1.Pod, recreates bool) bool {
	if leader == nil || leader.DeletionTimestamp != nil || annotated(leader, recreatePending) {
		return true
	}
	for index := range pods {
		pod := &pods[index]
		if pod.CreationTimestamp.Before(&leader.CreationTimestamp) {
			return true
		}
		if recreates && (pod.DeletionTimestamp != nil || restarted(pod)) {
			return true
		}
	}
	return false
}

func annotated(pod *corev1.Pod, key string) bool {
	if pod == nil {
		return false
	}
	_, ok := pod.Annotations[key]
	return ok
}

func identity(pod *corev1.Pod) string {
	hash := sha256.New()
	hash.Write([]byte(pod.UID))
	list := statuses(pod)
	for index := range list {
		fmt.Fprintf(hash, "/%s/%s/%d", list[index].Name, list[index].ContainerID, list[index].RestartCount)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func state(pod *corev1.Pod) workload.PodState {
	switch {
	case pod.DeletionTimestamp != nil:
		return workload.PodTerminating
	case running(pod) && net.ParseIP(pod.Status.PodIP) != nil:
		return workload.PodReady
	}
	return workload.PodStarting
}

func running(pod *corev1.Pod) bool {
	if len(pod.Spec.Containers) == 0 {
		return false
	}
	for index := range pod.Spec.InitContainers {
		status, ok := find(pod.Status.InitContainerStatuses, pod.Spec.InitContainers[index].Name)
		done := ok && status.State.Terminated != nil && status.State.Terminated.ExitCode == 0
		if !ok || status.ContainerID == "" || (status.State.Running == nil && !done) {
			return false
		}
	}
	for index := range pod.Spec.Containers {
		status, ok := find(pod.Status.ContainerStatuses, pod.Spec.Containers[index].Name)
		if !ok || status.ContainerID == "" || status.State.Running == nil {
			return false
		}
	}
	return true
}

func restarted(pod *corev1.Pod) bool {
	list := statuses(pod)
	for index := range list {
		if list[index].RestartCount > 0 {
			return true
		}
	}
	for index := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[index].State.Terminated != nil {
			return true
		}
	}
	return false
}

func statuses(pod *corev1.Pod) []corev1.ContainerStatus {
	var result []corev1.ContainerStatus
	for index := range pod.Spec.InitContainers {
		if status, ok := find(pod.Status.InitContainerStatuses, pod.Spec.InitContainers[index].Name); ok {
			result = append(result, status)
		}
	}
	for index := range pod.Spec.Containers {
		if status, ok := find(pod.Status.ContainerStatuses, pod.Spec.Containers[index].Name); ok {
			result = append(result, status)
		}
	}
	return result
}

func find(list []corev1.ContainerStatus, name string) (corev1.ContainerStatus, bool) {
	index := slices.IndexFunc(list, func(status corev1.ContainerStatus) bool { return status.Name == name })
	if index < 0 {
		return corev1.ContainerStatus{}, false
	}
	return list[index], true
}
