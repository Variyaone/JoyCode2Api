package pricing

import (
	"net/url"
	"testing"
)

func TestRatesAndAliases(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Rates {
		if seen[r.Model] {
			t.Fatal("duplicate model")
		}
		seen[r.Model] = true
		if (r.Input == nil) != (r.Output == nil) {
			t.Fatal("incomplete rate")
		}
		if r.Input != nil {
			u, e := url.Parse(r.URL)
			if e != nil || u.Scheme != "https" || r.Source == "" || *r.Input < 0 || *r.Output < 0 {
				t.Fatal("invalid provenance/rate")
			}
		}
	}
	if Lookup("GPT-unknown").Input != nil {
		t.Fatal("unknown prefix was priced")
	}
	if Lookup("Kimi-K3-jcloud").Input == nil {
		t.Fatal("jcloud model must inherit vendor list price")
	}
	if *Lookup("Claude-Opus-5-hq").Input != 50 {
		t.Fatal("explicit alias missing")
	}
}
