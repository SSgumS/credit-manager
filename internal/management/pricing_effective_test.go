package management

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuluo688/credit-manager/internal/config"
	"github.com/yuluo688/credit-manager/internal/service"

	_ "modernc.org/sqlite"
)

func openEffectivePricingService(t *testing.T) (*service.Service, context.Context) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("CREDIT_MANAGER_TEST_PEPPERS", "active:0123456789abcdef0123456789abcdef")
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.Keys.PepperEnv = "CREDIT_MANAGER_TEST_PEPPERS"
	cfg.Keys.ActivePepperID = "active"
	svc, err := service.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc, ctx
}

// The pair cap is contractual: one keeper sync round must stay a single
// bounded read transaction, so 1000 succeeds and 1001 fails with 400.
func TestPostPricingEffectivePairCap(t *testing.T) {
	svc, ctx := openEffectivePricingService(t)

	pairs := make([]map[string]string, 0, 1000)
	for index := 0; index < 1000; index++ {
		pairs = append(pairs, map[string]string{"provider": "p", "model": fmt.Sprintf("model-%d", index)})
	}
	body, err := json.Marshal(map[string]any{"items": pairs})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := postPricingEffective(ctx, svc, body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("1000 pairs status = %d body=%s", resp.StatusCode, resp.Body)
	}
	var snapshot struct {
		Revision string `json:"revision"`
		Items    []struct {
			Model  string `json:"model"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1000 {
		t.Fatalf("items = %d, want 1000", len(snapshot.Items))
	}
	if !strings.HasPrefix(snapshot.Revision, "sha256:") {
		t.Fatalf("revision missing sha256 prefix: %q", snapshot.Revision)
	}

	pairs = append(pairs, map[string]string{"provider": "p", "model": "one-too-many"})
	body, err = json.Marshal(map[string]any{"items": pairs})
	if err != nil {
		t.Fatal(err)
	}
	resp, err = postPricingEffective(ctx, svc, body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("1001 pairs status = %d, want 400", resp.StatusCode)
	}
}

func TestPostPricingEffectiveMalformedBody(t *testing.T) {
	svc, ctx := openEffectivePricingService(t)
	resp, err := postPricingEffective(ctx, svc, []byte(`{not json`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed status = %d, want 400", resp.StatusCode)
	}
	resp, err = postPricingEffective(ctx, svc, []byte(`{"items":[{"provider":"p"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing model status = %d, want 400", resp.StatusCode)
	}
}

// A corrupt stored regex is a server fault: the rulebook cannot be interpreted
// and a partial snapshot is never acceptable, so the contract maps it to 500.
func TestPostPricingEffectiveInvalidStoredRegexYields500(t *testing.T) {
	svc, ctx := openEffectivePricingService(t)

	dsn := "file:" + filepath.ToSlash(filepath.Join(svc.Config().DataDir, "credit-manager.db")) + "?_pragma=busy_timeout(5000)"
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `INSERT INTO pricing_rules(
		id, match_kind, pattern, priority,
		input_per_mtok_micro_usd, output_per_mtok_micro_usd, reasoning_per_mtok_micro_usd,
		cached_per_mtok_micro_usd, cache_read_per_mtok_micro_usd, cache_creation_per_mtok_micro_usd,
		enabled, created_at_unix_ms, updated_at_unix_ms
	) VALUES ('corrupt', 'regexp', '([', 100, 0, 0, 0, 0, 0, 0, 1, 1, 1)`); err != nil {
		t.Fatalf("seed corrupt regex rule: %v", err)
	}

	resp, err := postPricingEffective(ctx, svc, []byte(`{"items":[{"provider":"p","model":"any-model"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("corrupt regex status = %d body=%s, want 500", resp.StatusCode, resp.Body)
	}
}

// Resolution consistency is pinned end to end: repeated calls with an
// unchanged rulebook must return byte-identical revisions.
func TestPostPricingEffectiveRevisionDeterministic(t *testing.T) {
	svc, ctx := openEffectivePricingService(t)
	body := []byte(`{"items":[{"provider":"p","model":"unmatched"}]}`)

	first, err := postPricingEffective(ctx, svc, body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := postPricingEffective(ctx, svc, body)
	if err != nil {
		t.Fatal(err)
	}
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
		t.Fatalf("statuses = %d/%d, want 200/200", first.StatusCode, second.StatusCode)
	}
	var a, b struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(first.Body, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body, &b); err != nil {
		t.Fatal(err)
	}
	if a.Revision != b.Revision {
		t.Fatalf("revision drifted across identical reads: %s vs %s", a.Revision, b.Revision)
	}
}
