// Copyright (c) the appkit authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !darwin

package appkit

// Away from macOS there is no AppKit and no control to embed. platformCreate
// answers that it cannot, rather than being absent: a consumer that
// cross-compiles gets the same constructors and one clean [ErrUnsupported] out
// of them, instead of a build that fails on a platform where the feature was
// never going to exist. Everything above the seam — kind and spec validation,
// the action registry, the closed-state bookkeeping — is in the portable file
// and behaves here exactly as it does on macOS, which is what lets every branch
// of it be tested on a Linux runner with no window server.
func platformCreate(Spec, uint64) (impl, error) { return nil, ErrUnsupported }
