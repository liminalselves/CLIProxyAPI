package auth

import (
	"bytes"
	"strings"

	"github.com/tidwall/gjson"
)

// Only explicitly empty startup events are safe to discard before failover.
// Token/TTFT detection is insufficient: server-side tools can start before tokens.
type streamPrelude struct {
	pending []byte
}

func (p *streamPrelude) hasOutput(chunk []byte) bool {
	p.pending = normalizeGluedSSEEvents(append(p.pending, chunk...))
	for len(p.pending) > 0 {
		line, rest, complete := bytes.Cut(p.pending, []byte("\n"))
		line = bytes.TrimSpace(line)
		if !complete && !completePreludeLine(line) {
			return !possiblePreludeLine(line)
		}
		if !isStreamPreludeLine(line) {
			return true
		}
		p.pending = rest
	}
	return false
}

func completePreludeLine(line []byte) bool {
	if len(line) == 0 || gjson.ValidBytes(line) {
		return true
	}
	_, data, ok := extractSSEDataLine(line)
	return ok && (len(bytes.TrimSpace(data)) == 0 || gjson.ValidBytes(data))
}

func possiblePreludeLine(line []byte) bool {
	if len(line) == 0 || line[0] == '{' || line[0] == ':' {
		return true
	}
	for _, prefix := range []string{"data:", "event:", "id:", "retry:"} {
		if bytes.HasPrefix(line, []byte(prefix)) || bytes.HasPrefix([]byte(prefix), line) {
			return true
		}
	}
	return false
}

func isStreamPreludeLine(line []byte) bool {
	if len(line) == 0 || line[0] == ':' {
		return true
	}
	if event, ok := bytes.CutPrefix(line, []byte("event:")); ok {
		return isStreamPreludeEvent(strings.TrimSpace(string(event)))
	}
	if bytes.HasPrefix(line, []byte("id:")) || bytes.HasPrefix(line, []byte("retry:")) {
		return true
	}
	if _, data, ok := extractSSEDataLine(line); ok {
		line = bytes.TrimSpace(data)
		if len(line) == 0 {
			return true
		}
	}
	if !gjson.ValidBytes(line) {
		return false
	}
	root := gjson.ParseBytes(line)
	switch root.Get("type").String() {
	case "response.created", "response.in_progress":
		return emptyPreludeValue(root.Get("response.output")) && emptyPreludeValue(root.Get("response.error"))
	case "response.output_item.added":
		item := root.Get("item")
		kind := item.Get("type").String()
		return (kind == "message" || kind == "reasoning") &&
			emptyPreludeValue(item.Get("content")) && emptyPreludeValue(item.Get("summary")) &&
			emptyPreludeValue(item.Get("encrypted_content"))
	case "response.content_part.added", "response.reasoning_summary_part.added":
		part := root.Get("part")
		switch part.Get("type").String() {
		case "output_text", "text", "reasoning_text", "summary_text":
			return emptyPreludeValue(part.Get("text")) && emptyPreludeValue(part.Get("refusal"))
		}
		return false
	case "message_start":
		return emptyPreludeValue(root.Get("message.content")) && emptyPreludeValue(root.Get("message.stop_reason"))
	case "content_block_start":
		block := root.Get("content_block")
		kind := block.Get("type").String()
		return (kind == "text" || kind == "thinking") && emptyPreludeValue(block.Get("text")) &&
			emptyPreludeValue(block.Get("thinking")) && emptyPreludeValue(block.Get("signature"))
	case "ping", "keepalive", "codex.rate_limits", "codex.response.metadata":
		return true
	case "":
		choices := root.Get("choices")
		if !choices.IsArray() || len(choices.Array()) == 0 {
			return false
		}
		for _, choice := range choices.Array() {
			if !emptyPreludeValue(choice.Get("finish_reason")) || !emptyPreludeValue(choice.Get("text")) {
				return false
			}
			delta := choice.Get("delta")
			if !delta.IsObject() {
				return false
			}
			safe := true
			delta.ForEach(func(key, value gjson.Result) bool {
				switch key.String() {
				case "role":
					safe = value.String() == "assistant" || emptyPreludeValue(value)
				case "content", "reasoning", "reasoning_content":
					safe = emptyPreludeValue(value)
				default:
					safe = false
				}
				return safe
			})
			if !safe {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func emptyPreludeValue(value gjson.Result) bool {
	return value.Type == gjson.Null || (value.Type == gjson.String && value.Str == "") ||
		(value.IsArray() && len(value.Array()) == 0)
}

func isStreamPreludeEvent(event string) bool {
	switch event {
	case "response.created", "response.in_progress", "response.output_item.added",
		"response.content_part.added", "response.reasoning_summary_part.added",
		"message_start", "content_block_start", "ping", "keepalive",
		"codex.rate_limits", "codex.response.metadata":
		return true
	default:
		return false
	}
}
