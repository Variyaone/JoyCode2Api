package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/vibe-coding-labs/JoyCode2Api/pkg/pricing"
)

// CostRow survives raw-log retention. Amounts are integral tenths of a
// micro-dollar; USD conversion happens only at the presentation boundary.
type CostRow struct {
	Day          string `json:"day"`
	Model        string `json:"model"`
	PriceVersion string `json:"price_version"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	MissingUsage int64  `json:"missing_usage"`
	InputRate    *int64 `json:"input_rate"`
	OutputRate   *int64 `json:"output_rate"`
	Amount       *int64 `json:"amount_tenth_micro_usd"`
}

func addCostRow(tx *sql.Tx, day, model string, count, input, output, missing int64) error {
	rate := pricing.Lookup(model)
	var amount *int64
	if rate.Input != nil && rate.Output != nil {
		n := input**rate.Input + output**rate.Output
		amount = &n
	}
	_, err := tx.Exec(`INSERT INTO cost_daily
		(day, model, price_version, requests, input_tokens, output_tokens, missing_usage, input_rate, output_rate, amount)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day, model, price_version) DO UPDATE SET
		requests=requests+excluded.requests, input_tokens=input_tokens+excluded.input_tokens,
		output_tokens=output_tokens+excluded.output_tokens, missing_usage=missing_usage+excluded.missing_usage,
		amount=amount+excluded.amount`, day, model, pricing.Version, count, input, output, missing, rate.Input, rate.Output, amount)
	return err
}

func (s *Store) migrateCosts() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS cost_daily (
		day TEXT NOT NULL, model TEXT NOT NULL, price_version TEXT NOT NULL,
		requests INTEGER NOT NULL, input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL,
		missing_usage INTEGER NOT NULL, input_rate INTEGER, output_rate INTEGER, amount INTEGER,
		PRIMARY KEY(day,model,price_version))`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS cost_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return err
	}
	var done string
	err = tx.QueryRow("SELECT value FROM cost_meta WHERE key='initialized'").Scan(&done)
	if err == nil {
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}
	rows, err := tx.Query(`SELECT date(created_at), COALESCE(model,''), COUNT(*),
		COALESCE(SUM(MAX(COALESCE(input_tokens,0),0)),0), COALESCE(SUM(MAX(COALESCE(output_tokens,0),0)),0),
		SUM(CASE WHEN COALESCE(input_tokens,0)<=0 AND COALESCE(output_tokens,0)<=0 THEN 1 ELSE 0 END)
		FROM request_logs WHERE date(created_at) IS NOT NULL GROUP BY date(created_at),COALESCE(model,'')`)
	if err != nil {
		return err
	}
	var history []CostRow
	for rows.Next() {
		var r CostRow
		if err := rows.Scan(&r.Day, &r.Model, &r.Requests, &r.InputTokens, &r.OutputTokens, &r.MissingUsage); err != nil {
			rows.Close()
			return err
		}
		history = append(history, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range history {
		if err := addCostRow(tx, r.Day, r.Model, r.Requests, r.InputTokens, r.OutputTokens, r.MissingUsage); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("INSERT INTO cost_meta(key,value) VALUES('initialized',?)", time.Now().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// GetCostRows returns compact day/model aggregates, never prompts, keys or
// account identifiers. Existing price versions are preserved on upgrades.
func (s *Store) GetCostRows() ([]CostRow, error) {
	rows, err := s.db.Query(`SELECT day,model,price_version,requests,input_tokens,output_tokens,missing_usage,input_rate,output_rate,amount FROM cost_daily ORDER BY day DESC,model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CostRow{}
	for rows.Next() {
		var r CostRow
		if err := rows.Scan(&r.Day, &r.Model, &r.PriceVersion, &r.Requests, &r.InputTokens, &r.OutputTokens, &r.MissingUsage, &r.InputRate, &r.OutputRate, &r.Amount); err != nil {
			return nil, fmt.Errorf("read costs: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
