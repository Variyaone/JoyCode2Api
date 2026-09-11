package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibe-coding-labs/JoyCode2Api/pkg/joycode"
)

func TestResponsesHandlerSendsMidTurnInstruction(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			client := joycode.NewClient("synthetic", "synthetic")
			calls := 0
			client.SetHTTPClient(&http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				input, _ := body["input"].([]interface{})
				if len(input) != 2 {
					t.Fatalf("upstream input lost message: %v", body)
				}
				last := input[1].(map[string]interface{})
				content, _ := json.Marshal(last)
				if last["role"] != "system" || !strings.Contains(string(content), "DELIVERY_HANDLER_MARKER") {
					t.Fatalf("queued text missing upstream: %s", content)
				}
				sse := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"received\"}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(sse))}, nil
			})})
			mux := http.NewServeMux()
			NewHandler(client, nil).RegisterRoutes(mux)
			payload := fmt.Sprintf(`{"model":"GPT-6 Astra","stream":%t,"max_tokens":8192,"system":"base","messages":[{"role":"user","content":"initial task"},{"role":"system","content":"The user sent a new message while you were working: DELIVERY_HANDLER_MARKER"}]}`, stream)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(payload)))
			if calls != 1 || w.Code != 200 || !strings.Contains(w.Body.String(), "received") {
				t.Fatalf("calls=%d code=%d response=%s", calls, w.Code, w.Body.String())
			}
		})
	}
}

func TestResponsesMidTurnInstructionSurvivesTranslation(t *testing.T) {
	for _, role := range []string{"system", "developer", "user"} {
		t.Run(role, func(t *testing.T) {
			// CLI 2.1.260 emits queued_command attachments as mid-conversation
			// system messages, not necessarily as ordinary user text.
			req := &MessageRequest{
				Model: "GPT-6 Astra", Stream: true, MaxTokens: 8192,
				System: json.RawMessage(`"base policy"`),
				Messages: []MessageParam{
					{Role: "user", Content: json.RawMessage(`"initial task"`)},
					{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"call_test","name":"test","input":{}}]`)},
					{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call_test","content":"done"}]`)},
					{Role: role, Content: json.RawMessage(`"The user sent a new message while you were working:\nDELIVERY_MARKER"`)},
				},
			}
			if PreemptiveTruncate(req) != 0 {
				t.Fatal("small request unexpectedly truncated")
			}
			body := joycode.ChatToResponses(TranslateRequest(req, "", ""))
			input := body["input"].([]interface{})
			if len(input) != 4 {
				t.Fatalf("input has %d items; queued instruction lost: %v", len(input), input)
			}
			last := input[3].(map[string]interface{})
			encoded, _ := json.Marshal(last)
			if last["role"] != role || !strings.Contains(string(encoded), "DELIVERY_MARKER") {
				t.Fatalf("queued instruction altered: %s", encoded)
			}
			if input[2].(map[string]interface{})["type"] != "function_call_output" {
				t.Fatal("tool output order changed")
			}
		})
	}
}
