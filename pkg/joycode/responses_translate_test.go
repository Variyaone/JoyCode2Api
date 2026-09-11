package joycode

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChatToResponsesPreservesInstructionOrder(t *testing.T) {
	for _, tc := range []struct {
		name         string
		messages     string
		instructions string
		roles        []string
		texts        []string
	}{
		{
			name: "mid-turn system and developer",
			messages: `[
				{"role":"system","content":"base instructions"},
				{"role":"user","content":"initial task"},
				{"role":"system","content":"The user sent a new message while you were working: keep Windows and macOS"},
				{"role":"developer","content":[{"type":"text","text":"second queued requirement"}]},
				{"role":"user","content":"continue"}
			]`,
			instructions: "base instructions",
			roles:        []string{"user", "system", "developer", "user"},
			texts:        []string{"initial task", "The user sent a new message while you were working: keep Windows and macOS", "second queued requirement", "continue"},
		},
		{
			name:     "no initial system does not hoist mid-turn message",
			messages: `[{"role":"user","content":"first"},{"role":"system","content":"later"}]`,
			roles:    []string{"user", "system"}, texts: []string{"first", "later"},
		},
		{
			name:         "multiple initial instructions remain distinct",
			messages:     `[{"role":"system","content":"base"},{"role":"developer","content":"policy"},{"role":"system","content":"extra"},{"role":"user","content":"task"}]`,
			instructions: "base",
			roles:        []string{"developer", "system", "user"}, texts: []string{"policy", "extra", "task"},
		},
		{
			name:         "ordinary conversation unchanged",
			messages:     `[{"role":"system","content":"base"},{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]`,
			instructions: "base",
			roles:        []string{"user", "assistant"}, texts: []string{"hello", "hi"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages []interface{}
			if err := json.Unmarshal([]byte(tc.messages), &messages); err != nil {
				t.Fatal(err)
			}
			body := ChatToResponses(map[string]interface{}{"model": "GPT-6 Astra", "messages": messages, "stream": true})
			instructions, _ := body["instructions"].(string)
			if instructions != tc.instructions {
				t.Fatalf("instructions = %q, want %q", instructions, tc.instructions)
			}
			var roles, texts []string
			for _, item := range body["input"].([]interface{}) {
				m := item.(map[string]interface{})
				roles = append(roles, m["role"].(string))
				for _, part := range m["content"].([]map[string]interface{}) {
					texts = append(texts, part["text"].(string))
				}
			}
			if !reflect.DeepEqual(roles, tc.roles) || !reflect.DeepEqual(texts, tc.texts) {
				t.Fatalf("roles/texts = %v / %q, want %v / %q", roles, texts, tc.roles, tc.texts)
			}
		})
	}
}
