// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kitchen

import "math"

func f32bits(f float32) uint32 { return math.Float32bits(f) }
func f64bits(f float64) uint64 { return math.Float64bits(f) }
