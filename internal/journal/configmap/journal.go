// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package configmap stores the journal in a ConfigMap owned by the workload object.
package configmap

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/llm-d/llm-d-resiliency-operator/internal/journal"
)

const key = "state.json"

type Journal struct {
	reader client.Reader
	writer client.Client
	owner  *unstructured.Unstructured
	name   types.NamespacedName
	stored *corev1.ConfigMap
}

func New(reader client.Reader, writer client.Client, owner *unstructured.Unstructured) *Journal {
	return &Journal{
		reader: reader, writer: writer, owner: owner,
		name: types.NamespacedName{Namespace: owner.GetNamespace(), Name: owner.GetName() + "-ft-state"},
	}
}

func (store *Journal) Load(ctx context.Context) ([]byte, error) {
	var current corev1.ConfigMap
	err := store.reader.Get(ctx, store.name, &current)
	if apierrors.IsNotFound(err) {
		store.stored = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	store.stored = &current
	if data := current.Data[key]; data != "" {
		return []byte(data), nil
	}
	return nil, nil
}

func (store *Journal) Save(ctx context.Context, data []byte) error {
	err := store.write(ctx, data)
	switch {
	case apierrors.IsNotFound(err) && store.stored != nil:
		store.stored = nil
		err = store.write(ctx, data)
	case apierrors.IsConflict(err) && store.stored != nil:
		// An edit that leaves the record alone, such as a new annotation, is not another writer.
		var current corev1.ConfigMap
		if getErr := store.reader.Get(ctx, store.name, &current); getErr == nil && current.Data[key] == store.stored.Data[key] {
			store.stored = &current
			err = store.write(ctx, data)
		}
	}
	if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("%w: %w", journal.ErrConflict, err)
	}
	return err
}

func (store *Journal) write(ctx context.Context, data []byte) error {
	next := store.stored.DeepCopy()
	if next == nil {
		owner := store.owner.DeepCopy()
		if err := store.reader.Get(ctx, client.ObjectKeyFromObject(owner), owner); err != nil {
			return err
		}
		next = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Namespace: store.name.Namespace, Name: store.name.Name,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: owner.GetAPIVersion(), Kind: owner.GetKind(), Name: owner.GetName(), UID: owner.GetUID(),
			}},
		}}
	}
	if next.Data == nil {
		next.Data = map[string]string{}
	}
	next.Data[key] = string(data)
	var err error
	if store.stored == nil {
		err = store.writer.Create(ctx, next)
	} else {
		err = store.writer.Update(ctx, next)
	}
	if err == nil {
		store.stored = next
	}
	return err
}
