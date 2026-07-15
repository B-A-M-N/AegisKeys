package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBridgeTranslatesMessageAndToolCalls(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected upstream request: %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got["model"] != "deepseek-test" {
			t.Fatalf("model = %#v", got["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat_1", "model": "deepseek-test", "choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "content": "thinking", "tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "shell", "arguments": "{\"command\":\"pwd\"}"}}}}}}})
	}))
	defer upstream.Close()
	b, err := Start(Config{TargetBaseURL: upstream.URL + "/v1", APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	body := `{"model":"deepseek-test","max_tokens":128,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"tools":[{"name":"shell","input_schema":{"type":"object"}}]}`
	resp, err := http.Post(b.URL()+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["stop_reason"] != "tool_use" {
		t.Fatalf("stop reason = %#v", out["stop_reason"])
	}
	content := out["content"].([]any)
	if len(content) != 2 || content[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("content = %#v", content)
	}
}
