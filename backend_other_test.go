// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !darwin

package appkit

import (
	"errors"
	"testing"
)

// TestPlatformCreateUnsupported covers the non-darwin backend: there is no
// AppKit, so the real seam reports [ErrUnsupported] for every kind rather than
// being absent. This is the whole of what exists off macOS besides the portable
// model, and the linux lane's gate requires it to be covered.
func TestPlatformCreateUnsupported(t *testing.T) {
	for k := Kind(0); k < kindCount; k++ {
		if _, err := platformCreate(Spec{Kind: k}, uint64(k)+1); !errors.Is(err, ErrUnsupported) {
			t.Errorf("platformCreate(%s) = %v, want ErrUnsupported", k, err)
		}
	}
}
