package joycode

import "testing"

// The upstream chat endpoint accepts only the id published as "chatApiModel";
// the JD- prefixed labels are display names that upstream rejects with 6001.
func TestResolveUpstreamTranslatesLabelToID(t *testing.T) {
	InstallCatalog([]ModelInfo{
		{Label: "JD-GLM-5.3", ModelID: "JD-GLM-5.3", ChatAPIModel: "GLM-5.3-jcloud"},
		{Label: "GLM-5.3", ModelID: "GLM-5.3", ChatAPIModel: "GLM-5.3"},
	})
	defer InstallCatalog(nil)

	cases := []struct{ in, want string }{
		{"JD-GLM-5.3", "GLM-5.3-jcloud"},
		{"GLM-5.3-jcloud", "GLM-5.3-jcloud"},
		{"GLM-5.3", "GLM-5.3"},
		{"Not-A-Model", "Not-A-Model"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ResolveUpstream(tc.in); got != tc.want {
			t.Errorf("ResolveUpstream(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A model the upstream just published must not be rewritten to the account
// default, even when it is missing from the built-in list.
func TestKnownModelUsesCatalog(t *testing.T) {
	InstallCatalog([]ModelInfo{
		{Label: "Brand-New-Model", ModelID: "Brand-New-Model", ChatAPIModel: "brand-new-id"},
	})
	defer InstallCatalog(nil)

	for _, name := range []string{"Brand-New-Model", "brand-new-id"} {
		if !KnownModel(name) {
			t.Errorf("KnownModel(%q) = false, want true", name)
		}
	}
	if KnownModel("Never-Published-Model") {
		t.Error("KnownModel(Never-Published-Model) = true, want false")
	}
}

func TestPrepareBodyTranslatesModel(t *testing.T) {
	InstallCatalog([]ModelInfo{
		{Label: "JD-GLM-5.3", ModelID: "JD-GLM-5.3", ChatAPIModel: "GLM-5.3-jcloud"},
	})
	defer InstallCatalog(nil)

	c := NewClient("ptkey", "userid")
	body := c.prepareBody(map[string]interface{}{
		"model":    "JD-GLM-5.3",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if body["model"] != "GLM-5.3-jcloud" {
		t.Errorf("prepareBody model = %v, want %q", body["model"], "GLM-5.3-jcloud")
	}
	// Endpoints without a model must be left alone.
	plain := c.prepareBody(map[string]interface{}{})
	if _, ok := plain["model"]; ok {
		t.Errorf("prepareBody invented a model field: %v", plain["model"])
	}
}

func TestPrepareAnthropicBodyTranslatesModel(t *testing.T) {
	InstallCatalog([]ModelInfo{
		{Label: "Claude-Opus-5", ModelID: "Claude-Opus-5", ChatAPIModel: "Claude-Opus-5-hq"},
	})
	defer InstallCatalog(nil)

	c := NewClient("ptkey", "userid")
	body := c.prepareAnthropicBody(map[string]interface{}{"model": "Claude-Opus-5"})
	if body["model"] != "Claude-Opus-5-hq" {
		t.Errorf("prepareAnthropicBody model = %v, want %q", body["model"], "Claude-Opus-5-hq")
	}
}

// Without a catalog the name is forwarded untouched: a failed catalog fetch
// must never break a request.
func TestResolveUpstreamWithoutCatalog(t *testing.T) {
	InstallCatalog(nil)
	if got := ResolveUpstream("JD-GLM-5.3"); got != "JD-GLM-5.3" {
		t.Errorf("ResolveUpstream = %q, want %q", got, "JD-GLM-5.3")
	}
}
