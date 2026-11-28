# CLAUDE.md

## Overview

Money arithmetic: exact cost computation and tiered pricing shared as the keeper-side oracle.

## Files

| File | Contents (WHAT) | Read When (WHEN) |
| --- | --- | --- |
| `money.go` | PricePerMTok and CostFor exact cost computation | Changing cost math |
| `tiers.go` | CostForTiered context/service tier resolution | Changing tiered pricing |
| `cost_vector_test.go` | Shared cost vector table (cache, tiers, per-image, accounting modes, ceil rounding) replayed by the keeper | Changing the cost contract |
| `money_test.go`, `tiers_test.go` | Money arithmetic tests | Verifying cost math |
