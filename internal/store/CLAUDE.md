# CLAUDE.md

## Overview

SQLite persistence layer: keys, ledger, pricing, quotas, and the effective-pricing snapshot source.

## Files

| File | Contents (WHAT) | Read When (WHEN) |
| --- | --- | --- |
| `store.go` | Store construction and connection handling | Changing store setup |
| `pricing_effective.go` | EffectivePricingSnapshotFor: one read transaction resolving all pairs plus deterministic sha256 revision | Changing effective pricing |
| `pricing.go` | Price rule CRUD and winner selection | Changing pricing rules |
| `keys.go` | Key records and lifecycle | Changing key storage |
| `callers.go` | Caller (key owner) records | Changing caller storage |
| `usage.go` | Usage ledger writes and queries | Changing the ledger |
| `attempt.go`, `execution.go`, `reserve.go`, `settle.go` | Request lifecycle: attempt, execution, reserve, settle | Changing settlement |
| `auth_quota.go`, `auth_quota_history.go` | OAuth quota windows and history | Changing quota storage |
| `auth_warmup.go`, `auth_warmup_settings.go` | Warmup state and settings | Changing warmup |
| `auth_concurrency.go`, `auth_routing_settings.go` | Per-auth concurrency and routing | Changing concurrency |
| `audit.go` | Audit events | Changing audit |
| `handover.go` | Key handover records | Changing handover |
| `spend_reset.go` | Spend counter resets | Changing spend resets |
| `model_token_limits.go` | Per-model token limits | Changing token limits |
| `migrations.go` | Schema migrations | Changing schema |
| `scan.go` | Row scanning helpers | Changing queries |
| `*_test.go` | Store tests named for their subject | Verifying store behavior |
