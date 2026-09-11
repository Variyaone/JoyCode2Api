package store

import (
	"github.com/vibe-coding-labs/JoyCode2Api/pkg/pricing"
	"testing"
)

func TestCostsFormulaUnknownAndRetention(t *testing.T) {
	s := openTestStore(t)
	for _, r := range []struct {
		model   string
		in, out int
	}{{"GLM-5.3", 1000000, 1000000}, {"Kimi-K3-jcloud", 100, 20}, {"GPT-6 Astra", 0, 0}} {
		if err := s.LogRequest("u", r.model, "/v1/messages", true, 200, 1, "", r.in, r.out); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.GetCostRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		if r.Model == "GLM-5.3" && (r.Amount == nil || *r.Amount != 58000000) {
			t.Fatalf("want $5.80, got %+v", r)
		}
		if r.Model == "Kimi-K3-jcloud" && r.Amount != nil {
			t.Fatal("unknown price must be null")
		}
		if r.Model == "GPT-6 Astra" && r.MissingUsage != 1 {
			t.Fatal("missing usage not tracked")
		}
	}
	s.db.Exec("UPDATE request_logs SET created_at='2000-01-01 00:00:00'")
	if _, err := s.CleanupOldLogs(1); err != nil {
		t.Fatal(err)
	}
	again, err := s.GetCostRows()
	if err != nil || len(again) != 3 {
		t.Fatal("retention lost ledger", err)
	}
}

func TestCostBackfillIdempotentAndVersioned(t *testing.T) {
	s := openTestStore(t)
	s.db.Exec("DELETE FROM cost_meta WHERE key='initialized'")
	s.db.Exec("INSERT INTO request_logs(model,input_tokens,output_tokens,created_at) VALUES('GLM-5.3',10,20,'2020-01-01 23:59:59'),('GLM-5.3',30,40,'2020-01-02 00:00:00')")
	if err := s.migrateCosts(); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateCosts(); err != nil {
		t.Fatal(err)
	}
	rows, err := s.GetCostRows()
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, r := range rows {
		if r.Requests != 1 {
			t.Fatal("duplicate backfill")
		}
	}
	// A historical price snapshot must remain distinct from today's version.
	s.db.Exec("UPDATE cost_daily SET price_version='older-price',amount=123")
	if err := s.LogRequest("u", "GLM-5.3", "/v1/messages", false, 500, 1, "upstream failed", 1, 1); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.GetCostRows()
	if len(rows) != 3 {
		t.Fatal("old version overwritten")
	}
	for _, r := range rows {
		if r.PriceVersion == pricing.Version && (r.Amount == nil || *r.Amount != 58) {
			t.Fatal("tiny request rounded incorrectly")
		}
	}
}

func TestCostLogTransactionRollback(t *testing.T) {
	s := openTestStore(t)
	s.db.Exec("DROP TABLE cost_daily")
	if err := s.LogRequest("u", "GLM-5.3", "/v1/messages", false, 200, 1, "", 5, 5); err == nil {
		t.Fatal("expected failure")
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM request_logs").Scan(&count)
	if count != 0 {
		t.Fatal("partial request log committed")
	}
}
