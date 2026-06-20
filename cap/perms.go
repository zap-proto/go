// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cap

// Permission bits for Capability.Permissions (u64). Per
// capabilities_kinds.md "Permission bits": each CapKind owns the bottom
// 32 bits (their meaning is per-kind — verifiers MUST dispatch on
// CapKind first), and the top 32 bits are cross-cutting and identical
// across every CapKind.
//
// Only the cross-cutting bits are normative wire-wide and therefore
// defined here; the per-kind low bits are owned by each consumer
// (IAM/KMS/ATS/Bridge) and declared in capabilities_kinds.md.
const (
	// PermAttenuate (bit 1<<32) — the holder may mint child caps whose
	// permissions are a subset of this cap's. SPEC §2.3 step 3d: a parent
	// MUST carry this bit (or be CapKindDelegate) for the verifier to
	// accept any attenuation off it.
	PermAttenuate uint64 = 1 << 32
	// PermAudit (bit 1<<33) — the holder may read the audit trail for Target.
	PermAudit uint64 = 1 << 33
	// PermRoot (bit 1<<63) — root-of-trust marker, set on root caps only.
	PermRoot uint64 = 1 << 63
)
