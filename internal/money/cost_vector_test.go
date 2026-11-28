package money

import (
	"errors"
	"testing"
)

// The vector table is the shared oracle between credit-manager and the
// coworker keeper cost engine: both sides compute event costs from the same
// effective-pricing contract, so any drift in rounding, cache fallback, tier
// selection, or accounting mode must fail here before it can diverge in
// production. Keeper tests replay the same vectors verbatim.
// (ref: DL-005)
func TestCostVectorsSharedContract(t *testing.T) {
	type vector struct {
		name        string
		usage       TokenUsage
		price       PricePerMTok
		tiers       []PriceTier
		model       string
		provider    string
		serviceTier string
		want        MicroUSD
		wantErr     error
	}
	vectors := []vector{
		{
			name:  "plain input output ceil rounding",
			usage: TokenUsage{Input: 1, Output: 1},
			price: PricePerMTok{Input: 1, Output: 3},
			// ceil(1*1/1e6)=1 and ceil(1*3/1e6)=1: a positive priced usage is
			// never free, which is why per-class ceil rounding exists.
			want: 2,
		},
		{
			name:  "exact million boundary no rounding residue",
			usage: TokenUsage{Input: 1_000_000, Output: 2_000_000},
			price: PricePerMTok{Input: 100, Output: 250},
			want:  100 + 500,
		},
		{
			name:  "cache read falls back to cached counter and cached price",
			usage: TokenUsage{Input: 1000, Cached: 500},
			price: PricePerMTok{Input: 100, Cached: 10, AccountingMode: AccountingInputExcludesCache},
			model: "claude-3-5-sonnet",
			// ceil(1000*100/1e6)=1 input plus ceil(500*10/1e6)=1 cache read.
			want: 2,
		},
		{
			name:  "input includes cache subtracts cache tokens from input",
			usage: TokenUsage{Input: 1000, CacheRead: 400, CacheCreation: 100},
			price: PricePerMTok{Input: 1_000_000, CacheRead: 100_000, CacheCreation: 200_000, AccountingMode: AccountingInputIncludesCache},
			model: "gpt-5",
			// billable input 1000-(400+100)=500 -> 500; cache read 40; creation 20.
			want: 560,
		},
		{
			name:  "input excludes cache bills input independently",
			usage: TokenUsage{Input: 1000, CacheRead: 400},
			price: PricePerMTok{Input: 1_000_000, CacheRead: 100_000, AccountingMode: AccountingInputExcludesCache},
			model: "claude-opus-4",
			// input bills independently: 1000 plus cache read 40.
			want: 1040,
		},
		{
			name:  "context tier selects highest matching threshold",
			usage: TokenUsage{Input: 300_000},
			price: PricePerMTok{Input: 1_000_000},
			tiers: []PriceTier{
				{Kind: PriceTierContext, Threshold: 100_000, Price: PricePerMTok{Input: 2_000_000}},
				{Kind: PriceTierContext, Threshold: 200_000, Price: PricePerMTok{Input: 4_000_000}},
			},
			model: "gpt-5",
			// highest matching threshold 200k wins: 300000 tokens at 4 USD/Mtok.
			want: 1_200_000,
		},
		{
			name:  "service tier beats context tier",
			usage: TokenUsage{Input: 300_000},
			price: PricePerMTok{Input: 1_000_000},
			tiers: []PriceTier{
				{Kind: PriceTierContext, Threshold: 200_000, Price: PricePerMTok{Input: 4_000_000}},
				{Kind: PriceTierService, Service: "priority", Price: PricePerMTok{Input: 8_000_000}},
			},
			serviceTier: "priority",
			// service tier beats context tier: 300000 tokens at 8 USD/Mtok.
			want: 2_400_000,
		},
		{
			name:  "per image bills flat price times images",
			usage: TokenUsage{Images: 3},
			price: PricePerMTok{BillingMode: BillingPerImage, PerImage: 40_000},
			want:  120_000,
		},
		{
			name:  "per image zero images still bills a single image",
			usage: TokenUsage{},
			price: PricePerMTok{BillingMode: BillingPerImage, PerImage: 40_000},
			want:  40_000,
		},
		{
			name:    "negative usage rejected",
			usage:   TokenUsage{Input: -1},
			price:   PricePerMTok{Input: 100},
			wantErr: ErrNegativeValue,
		},
		{
			name:    "overflow surfaces instead of wrapping",
			usage:   TokenUsage{Input: 1 << 62},
			price:   PricePerMTok{Input: 1 << 40},
			wantErr: ErrOverflow,
		},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			got, err := CostForTiered(v.usage, v.price, v.tiers, v.model, v.provider, v.serviceTier)
			if v.wantErr != nil {
				if !errors.Is(err, v.wantErr) {
					t.Fatalf("CostForTiered error = %v, want %v", err, v.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CostForTiered unexpected error: %v", err)
			}
			if got != v.want {
				t.Fatalf("CostForTiered = %d micro-USD, want %d", got, v.want)
			}
		})
	}
}
