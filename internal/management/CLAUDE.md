# CLAUDE.md

## Overview

Management API routes, console pages, and browser resources served through the CPA plugin host.

## Files

| File | Contents (WHAT) | Read When (WHEN) |
| --- | --- | --- |
| `management.go` | Route registry and dispatch; registers `POST credit-manager/pricing/effective` | Adding management routes |
| `pricing_effective.go` | Effective pricing handler: complete per-model snapshot with sha256 revision | Changing the effective contract |
| `pricing.go` | Price rule management handlers | Changing pricing management |
| `keys.go` | Key issue/rotate/reveal/revoke/delete handlers | Changing key management |
| `callers.go` | Caller management handlers | Changing callers |
| `lookup.go` | `lookup/data` key self-service endpoint (recent_only probe) | Changing key lookup |
| `auth_quotas.go` | auth-quotas endpoints including pagination and concurrency | Changing auth quotas |
| `usage.go`, `overview.go` | Usage detail/summary and console overview | Changing usage views |
| `views.go`, `pages.go` | Console and lookup HTML pages | Changing management UI |
| `fx.go` | USD/CNY rate resource | Changing FX |
| `*_test.go` | Handler tests named for their subject | Verifying handlers |
