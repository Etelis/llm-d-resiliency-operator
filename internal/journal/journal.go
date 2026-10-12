// Copyright 2026 The llm-d Authors.
// SPDX-License-Identifier: Apache-2.0

// Package journal defines where the controller keeps its state between leaders.
// See docs/design/contracts/journal.md.
package journal

import (
	"context"
	"errors"
)

// Journal stores the controller's record for one engine group. The bytes are opaque:
// the controller owns their schema.
type Journal interface {
	// Load returns the saved record, or nil when there is none yet.
	Load(ctx context.Context) ([]byte, error)
	// Save writes the record unless another writer changed it since this process last
	// loaded or saved it, in which case it returns ErrConflict.
	Save(ctx context.Context, data []byte) error
}

// ErrConflict means someone else wrote the record since this process last loaded or saved it.
var ErrConflict = errors.New("journal conflict")
