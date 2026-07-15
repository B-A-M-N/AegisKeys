// Package bridge provides a local, ephemeral Anthropic Messages to OpenAI
// Chat Completions bridge. It exists for clients such as Free Code that use
// Anthropic's wire format even when the selected provider only exposes chat
// completions. The upstream key is held in memory and is never written to a
// config file or child environment.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	TargetBaseURL string
	APIKey        string
	ClientToken   string
}

type Server struct {
	listener net.Listener
	http     *http.Server
	url      string
	once     sync.Once
}

func Start(cfg Config) (*Server, error) {
	if strings.TrimSpace(cfg.TargetBaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("bridge requires target URL and API key")
	}
	if _, err := url.ParseRequestURI(cfg.TargetBaseURL); err != nil {
		return nil, fmt.Errorf("invalid bridge target URL: %w", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := &Server{listener: l, url: "http://" + l.Addr().String()}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", b.handleMessages(cfg))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	b.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = b.http.Serve(l) }()
	return b, nil
}

func (s *Server) URL() string { return s.url }

func (s *Server) Close() error {
	var err error
	s.once.Do(func() { err = s.http.Shutdown(context.Background()) })
	return err
}

func (s *Server) handleMessages(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cfg.ClientToken != "" && r.Header.Get("x-api-key") != cfg.ClientToken && r.Header.Get("Authorization") != "Bearer "+cfg.ClientToken {
			writeAnthropicError(w, http.StatusUnauthorized, "invalid local bridge credential")
			return
		}
		defer r.Body.Close()
		var req anthropicRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
			writeAnthropicError(w, http.StatusBadRequest, err.Error())
			return
		}
		openaiReq, err := toOpenAI(req)
		if err != nil {
			writeAnthropicError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Stream {
			openaiReq["stream"] = true
		}
		body, _ := json.Marshal(openaiReq)
		target := strings.TrimRight(cfg.TargetBaseURL, "/") + "/chat/completions"
		if !strings.HasSuffix(strings.TrimRight(cfg.TargetBaseURL, "/"), "/v1") {
			target = strings.TrimRight(cfg.TargetBaseURL, "/") + "/v1/chat/completions"
		}
		u, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			writeAnthropicError(w, 500, err.Error())
			return
		}
		u.Header.Set("Content-Type", "application/json")
		u.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		resp, err := http.DefaultClient.Do(u)
		if err != nil {
			writeAnthropicError(w, http.StatusBadGateway, err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			writeAnthropicError(w, resp.StatusCode, strings.TrimSpace(string(raw)))
			return
		}
		if req.Stream {
			streamOpenAI(w, resp.Body, req.Model)
			return
		}
		var upstream openAIResponse
		if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
			writeAnthropicError(w, 502, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fromOpenAI(upstream, req.Model))
	}
}

type anthropicRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    any              `json:"system"`
	Messages  []map[string]any `json:"messages"`
	Tools     []map[string]any `json:"tools"`
	Stream    bool             `json:"stream"`
}
type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string        `json:"finish_reason"`
		Message      openAIMessage `json:"message"`
	} `json:"choices"`
	Usage map[string]any `json:"usage"`
}
type openAIMessage struct {
	Role      string `json:"role"`
	Content   any    `json:"content"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func toOpenAI(r anthropicRequest) (map[string]any, error) {
	if r.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	msgs := make([]map[string]any, 0, len(r.Messages)+1)
	if r.System != nil {
		if s := contentText(r.System); s != "" {
			msgs = append(msgs, map[string]any{"role": "system", "content": s})
		}
	}
	for _, m := range r.Messages {
		role, _ := m["role"].(string)
		if role == "" {
			return nil, fmt.Errorf("message role is required")
		}
		blocks, ok := m["content"].([]any)
		if !ok {
			if s, ok := m["content"].(string); ok {
				msgs = append(msgs, map[string]any{"role": role, "content": s})
				continue
			}
			return nil, fmt.Errorf("invalid message content")
		}
		text := strings.Builder{}
		var calls []map[string]any
		for _, raw := range blocks {
			b, _ := raw.(map[string]any)
			typ, _ := b["type"].(string)
			switch typ {
			case "text":
				text.WriteString(contentText(b["text"]))
			case "tool_use":
				id, _ := b["id"].(string)
				name, _ := b["name"].(string)
				args, _ := json.Marshal(b["input"])
				calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(args)}})
			case "tool_result":
				id, _ := b["tool_use_id"].(string)
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": id, "content": contentText(b["content"])})
			}
		}
		if text.Len() > 0 || len(calls) > 0 {
			msg := map[string]any{"role": role, "content": text.String()}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			msgs = append(msgs, msg)
		}
	}
	out := map[string]any{"model": r.Model, "messages": msgs}
	if r.MaxTokens > 0 {
		out["max_tokens"] = r.MaxTokens
	}
	if len(r.Tools) > 0 {
		tools := make([]map[string]any, 0, len(r.Tools))
		for _, t := range r.Tools {
			name, _ := t["name"].(string)
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": name, "description": t["description"], "parameters": t["input_schema"]}})
		}
		out["tools"] = tools
	}
	return out, nil
}

func contentText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "text" {
					b.WriteString(contentText(m["text"]))
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

func fromOpenAI(r openAIResponse, fallback string) map[string]any {
	model := r.Model
	if model == "" {
		model = fallback
	}
	msg := openAIMessage{}
	stop := "end_turn"
	if len(r.Choices) > 0 {
		msg = r.Choices[0].Message
		if r.Choices[0].FinishReason == "tool_calls" {
			stop = "tool_use"
		}
	}
	content := []map[string]any{}
	if text := contentText(msg.Content); text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, c := range msg.ToolCalls {
		var input any = map[string]any{}
		_ = json.Unmarshal([]byte(c.Function.Arguments), &input)
		content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Function.Name, "input": input})
	}
	return map[string]any{"id": r.ID, "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": stop, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}
}

func writeAnthropicError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": message}})
}

func streamOpenAI(w http.ResponseWriter, body io.Reader, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, ok := w.(http.Flusher)
	if !ok {
		return
	}
	emit := func(event string, data any) {
		raw, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
		f.Flush()
	}
	emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "bridge", "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
	textStarted := false
	toolBlocks := map[int]int{}
	toolOrder := []int{}
	stopReason := "end_turn"
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		c := chunk.Choices[0]
		if c.FinishReason == "tool_calls" {
			stopReason = "tool_use"
		}
		if c.Delta.Content != "" {
			if !textStarted {
				emit("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
				textStarted = true
			}
			emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": c.Delta.Content}})
		}
		for _, call := range c.Delta.ToolCalls {
			block, exists := toolBlocks[call.Index]
			if !exists {
				block = len(toolBlocks)
				if textStarted {
					block++
				}
				toolBlocks[call.Index] = block
				toolOrder = append(toolOrder, call.Index)
				emit("content_block_start", map[string]any{"type": "content_block_start", "index": block, "content_block": map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": map[string]any{}}})
			}
			if call.Function.Arguments != "" {
				emit("content_block_delta", map[string]any{"type": "content_block_delta", "index": block, "delta": map[string]any{"type": "input_json_delta", "partial_json": call.Function.Arguments}})
			}
		}
	}
	if textStarted {
		emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	}
	for _, callIndex := range toolOrder {
		emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": toolBlocks[callIndex]})
	}
	emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 0}})
	emit("message_stop", map[string]any{"type": "message_stop"})
}
