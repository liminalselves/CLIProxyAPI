package auth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestStreamPreludeClassification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []string
		output bool
	}{
		{name: "split SSE", chunks: []string{"da", "ta: {\"type\":\"response.cre", "ated\"}\n\n"}},
		{name: "glued events", chunks: []string{`data: {"type":"response.created"}data: {"type":"response.in_progress"}`}},
		{name: "CRLF", chunks: []string{"event: message_start\r\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\r\n\r\n"}},
		{name: "raw JSON", chunks: []string{`{"type":"response.created"}`}},
		{name: "empty message", chunks: []string{`data: {"type":"response.output_item.added","item":{"type":"message","content":[]}}`}},
		{name: "empty reasoning", chunks: []string{`data: {"type":"response.output_item.added","item":{"type":"reasoning","summary":[]}}`}},
		{name: "empty text part", chunks: []string{`data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}`}},
		{name: "empty summary", chunks: []string{`data: {"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`}},
		{name: "heartbeat", chunks: []string{": ping\n\nid: 1\nretry: 100\nevent: ping\ndata: {\"type\":\"ping\"}\n\n"}},
		{name: "thinking", chunks: []string{`data: {"choices":[{"delta":{"reasoning_content":"hello"}}]}`}, output: true},
		{name: "tool delta", chunks: []string{`data: {"choices":[{"delta":{"tool_calls":[{"id":"call-1"}]}}]}`}, output: true},
		{name: "server tool", chunks: []string{`data: {"type":"response.output_item.added","item":{"type":"web_search_call"}}`}, output: true},
		{name: "encrypted reasoning", chunks: []string{`data: {"type":"response.output_item.added","item":{"type":"reasoning","encrypted_content":"ciphertext"}}`}, output: true},
		{name: "embedded content", chunks: []string{`data: {"type":"response.created","response":{"output":[{"type":"message"}]}}`}, output: true},
		{name: "embedded text", chunks: []string{`data: {"type":"content_block_start","content_block":{"type":"text","text":"hello"}}`}, output: true},
		{name: "unknown header", chunks: []string{"event: custom_tool\n"}, output: true},
		{name: "unknown JSON", chunks: []string{`{"type":"custom_tool"}`}, output: true},
		{name: "mixed frame", chunks: []string{"data: {\"type\":\"response.created\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"}, output: true},
		{name: "completion", chunks: []string{`data: {"type":"response.completed","response":{"output":[]}}`}, output: true},
		{name: "finish reason", chunks: []string{`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`}, output: true},
		{name: "done", chunks: []string{"data: [DONE]\n\n"}, output: true},
		{name: "unframed output", chunks: []string{"hello"}, output: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prelude streamPrelude
			output := false
			for _, chunk := range tc.chunks {
				if prelude.hasOutput([]byte(chunk)) {
					output = true
					break
				}
			}
			if output != tc.output {
				t.Fatalf("output = %v, want %v", output, tc.output)
			}
		})
	}
}

func TestReadStreamBootstrapPreservesPayload(t *testing.T) {
	prelude := cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"type\":\"response.created\"}\n\n")}
	content := cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")}
	for _, hold := range []bool{false, true} {
		ch := make(chan cliproxyexecutor.StreamChunk, 2)
		ch <- prelude
		ch <- content
		close(ch)
		got, closed, err := readStreamBootstrap(context.Background(), ch, hold)
		want := []cliproxyexecutor.StreamChunk{prelude}
		if hold {
			want = append(want, content)
		}
		if err != nil || closed || !reflect.DeepEqual(got, want) {
			t.Fatalf("hold=%v: chunks = %v, closed = %v, error = %v", hold, got, closed, err)
		}
	}
}

func TestReadStreamBootstrapCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan cliproxyexecutor.StreamChunk)
	result := make(chan error, 1)
	go func() {
		_, _, err := readStreamBootstrap(ctx, ch, true)
		result <- err
	}()
	ch <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"type\":\"response.created\"}\n\n")}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation while awaiting content", err)
	}
	close(ch)
}

func TestReadStreamBootstrapIncompleteFrame(t *testing.T) {
	for _, withError := range []bool{false, true} {
		ch := make(chan cliproxyexecutor.StreamChunk, 2)
		partial := cliproxyexecutor.StreamChunk{Payload: []byte(`data: {"type":"response.output_item.added","item":{"type":"web_search_call"`)}
		ch <- partial
		want := []cliproxyexecutor.StreamChunk{partial}
		if withError {
			failure := cliproxyexecutor.StreamChunk{Err: errors.New("connection lost")}
			ch <- failure
			want = append(want, failure)
		}
		close(ch)
		buffered, closed, err := readStreamBootstrap(context.Background(), ch, true)
		if err != nil || closed == withError || !reflect.DeepEqual(buffered, want) {
			t.Fatalf("withError=%v: buffered = %v, closed = %v, error = %v, want no replay of unknown partial output", withError, buffered, closed, err)
		}
	}
}

func TestReadStreamBootstrapLimit(t *testing.T) {
	ch := make(chan cliproxyexecutor.StreamChunk, 1)
	ch <- cliproxyexecutor.StreamChunk{Payload: []byte(":" + strings.Repeat("x", 1<<20) + "\n\n")}
	close(ch)
	buffered, _, err := readStreamBootstrap(context.Background(), ch, true)
	var authErr *Error
	if !errors.As(err, &authErr) || authErr.Code != "stream_bootstrap_limit" || len(buffered) != 0 {
		t.Fatalf("buffered = %d, error = %v, want bounded prelude failure", len(buffered), err)
	}
}
