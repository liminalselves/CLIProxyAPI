package multiuser

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

type AuditEvent struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Target string    `json:"target"`
	Status int       `json:"status"`
	IP     string    `json:"ip"`
	Detail string    `json:"detail,omitempty"`
}

type UsageEvent struct {
	ID                  string               `json:"id"`
	RequestID           string               `json:"request_id"`
	TraceID             string               `json:"trace_id"`
	At                  time.Time            `json:"at"`
	Consumer            string               `json:"consumer"`
	KeyID               string               `json:"key_id"`
	KeyName             string               `json:"key_name"`
	Owner               string               `json:"owner"`
	ChannelID           string               `json:"channel_id"`
	Channel             string               `json:"channel"`
	CredentialID        string               `json:"credential_id"`
	CredentialName      string               `json:"credential_name"`
	Provider            string               `json:"provider"`
	Model               string               `json:"model"`
	UpstreamModel       string               `json:"upstream_model"`
	Endpoint            string               `json:"endpoint"`
	Stream              bool                 `json:"stream"`
	Failed              bool                 `json:"failed"`
	Status              int                  `json:"status"`
	LatencyMS           int64                `json:"latency_ms"`
	TTFTMS              int64                `json:"ttft_ms"`
	InputTokens         int64                `json:"input_tokens"`
	OutputTokens        int64                `json:"output_tokens"`
	ReasoningTokens     int64                `json:"reasoning_tokens"`
	CacheReadTokens     int64                `json:"cache_read_tokens"`
	CacheCreationTokens int64                `json:"cache_creation_tokens"`
	TotalTokens         int64                `json:"total_tokens"`
	TokensKnown         bool                 `json:"tokens_known"`
	TokenBreakdown      usage.TokenBreakdown `json:"token_breakdown"`
}

type CallEvent struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	Consumer  string    `json:"consumer"`
	KeyID     string    `json:"key_id"`
	KeyName   string    `json:"key_name"`
	Endpoint  string    `json:"endpoint"`
	Model     string    `json:"model"`
	Status    int       `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
}

type journalEntry struct {
	Usage *UsageEvent `json:"usage,omitempty"`
	Audit *AuditEvent `json:"audit,omitempty"`
	Call  *CallEvent  `json:"call,omitempty"`
}

type Filter struct {
	Consumer     string
	Owner        string
	ChannelID    string
	CredentialID string
	KeyID        string
	Model        string
	Actor        string
	Action       string
	From         time.Time
	To           time.Time
	Page         int
	PageSize     int
}

func (s *Store) appendEntry(at time.Time, entry journalEntry) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	dir := filepath.Join(s.dir, "journal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, at.UTC().Format("2006-01-02")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	errEncode := json.NewEncoder(file).Encode(entry)
	if errEncode == nil {
		errEncode = file.Sync()
	}
	errClose := file.Close()
	if errEncode != nil {
		return errEncode
	}
	return errClose
}

func (s *Store) Audit(event AuditEvent) error {
	event.ID = uuid.NewString()
	if event.At.IsZero() {
		event.At = s.now().UTC()
	}
	return s.appendEntry(event.At, journalEntry{Audit: &event})
}

func (s *Store) RecordCall(event CallEvent) error {
	if event.At.IsZero() {
		event.At = s.now().UTC()
	}
	return s.appendEntry(event.At, journalEntry{Call: &event})
}

func (s *Store) HandleUsage(ctx context.Context, record usage.Record) {
	a := record.Attribution
	if a.Consumer == "" || a.SiteID != s.SiteID() {
		return
	}
	at := record.RequestedAt
	if at.IsZero() {
		at = s.now().UTC()
	}
	status := record.Fail.StatusCode
	if !record.Failed {
		status = 200
	} else if status == 0 {
		status = 502
	}
	d := record.Detail
	event := UsageEvent{
		ID: uuid.NewString(), RequestID: record.RequestID, TraceID: record.TraceID, At: at.UTC(),
		Consumer: a.Consumer, KeyID: a.KeyID, KeyName: a.KeyName, Owner: a.Owner,
		ChannelID: a.ChannelID, Channel: a.Channel, CredentialID: record.AuthID, CredentialName: a.CredentialName,
		Provider: record.Provider, Model: record.Alias, UpstreamModel: record.Model,
		Endpoint: logging.GetEndpoint(ctx), Stream: record.Stream, Failed: record.Failed, Status: status,
		LatencyMS: record.Latency.Milliseconds(), TTFTMS: record.TTFT.Milliseconds(),
		InputTokens: d.InputTokens, OutputTokens: d.OutputTokens, ReasoningTokens: d.ReasoningTokens,
		CacheReadTokens: d.CacheReadTokens, CacheCreationTokens: d.CacheCreationTokens, TotalTokens: d.TotalTokens,
		TokensKnown:    d.TotalTokens != 0 || d.InputTokens != 0 || d.OutputTokens != 0,
		TokenBreakdown: d.TokenBreakdown,
	}
	if d.TokenBreakdown.Valid() && d.TokenBreakdown.Quality == usage.TokenAccountingQualityComplete {
		b := d.TokenBreakdown
		event.InputTokens = b.Input.TotalTokens
		event.OutputTokens = b.Output.TotalTokens
		event.CacheReadTokens = b.Input.CacheReadTokens
		event.CacheCreationTokens = b.Input.CacheWriteTokens
		event.ReasoningTokens = b.Output.ReasoningTokens
		event.TotalTokens = b.TotalTokens
	}
	if event.Model == "" {
		event.Model = a.Channel + "/" + record.Model
	}
	if err := s.appendEntry(at, journalEntry{Usage: &event}); err != nil {
		log.WithError(err).Error("multi-user: failed to persist usage")
	}
}

func (s *Store) scan(filter Filter, visit func(journalEntry)) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.dir, "journal"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		day, errDate := time.Parse("2006-01-02", strings.TrimSuffix(entry.Name(), ".jsonl"))
		if errDate != nil {
			continue
		}
		if !filter.From.IsZero() && !day.Add(24*time.Hour).After(filter.From) {
			continue
		}
		if !filter.To.IsZero() && day.After(filter.To) {
			continue
		}
		file, errOpen := os.Open(filepath.Join(s.dir, "journal", entry.Name()))
		if errOpen != nil {
			return errOpen
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var value journalEntry
			if errDecode := json.Unmarshal(scanner.Bytes(), &value); errDecode != nil {
				_ = file.Close()
				return fmt.Errorf("invalid journal %s line %d: %w", entry.Name(), line, errDecode)
			}
			visit(value)
		}
		errScan := scanner.Err()
		errClose := file.Close()
		if errScan != nil {
			return errScan
		}
		if errClose != nil {
			return errClose
		}
	}
	return nil
}

func within(at time.Time, filter Filter) bool {
	return (filter.From.IsZero() || !at.Before(filter.From)) && (filter.To.IsZero() || at.Before(filter.To))
}

func matchUsage(event UsageEvent, filter Filter) bool {
	return within(event.At, filter) && (filter.Consumer == "" || event.Consumer == filter.Consumer) &&
		(filter.Owner == "" || event.Owner == filter.Owner) && (filter.ChannelID == "" || event.ChannelID == filter.ChannelID) &&
		(filter.CredentialID == "" || event.CredentialID == filter.CredentialID) && (filter.KeyID == "" || event.KeyID == filter.KeyID) &&
		(filter.Model == "" || strings.Contains(strings.ToLower(event.Model), strings.ToLower(filter.Model)))
}

func pageBounds(filter Filter, length int) (int, int) {
	page, size := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	start := (page - 1) * size
	if start > length {
		start = length
	}
	end := start + size
	if end > length {
		end = length
	}
	return start, end
}

func (s *Store) Usage(filter Filter) ([]UsageEvent, int, error) {
	out := make([]UsageEvent, 0)
	err := s.scan(filter, func(entry journalEntry) {
		if entry.Usage != nil && matchUsage(*entry.Usage, filter) {
			out = append(out, *entry.Usage)
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	start, end := pageBounds(filter, len(out))
	return out[start:end], len(out), err
}

func (s *Store) Calls(filter Filter) ([]CallEvent, int, error) {
	out := make([]CallEvent, 0)
	err := s.scan(filter, func(entry journalEntry) {
		if entry.Call == nil {
			return
		}
		event := *entry.Call
		if within(event.At, filter) && (filter.Consumer == "" || event.Consumer == filter.Consumer) && (filter.KeyID == "" || event.KeyID == filter.KeyID) && (filter.Model == "" || strings.Contains(event.Model, filter.Model)) {
			out = append(out, event)
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	start, end := pageBounds(filter, len(out))
	return out[start:end], len(out), err
}

func (s *Store) Audits(filter Filter) ([]AuditEvent, int, error) {
	out := make([]AuditEvent, 0)
	err := s.scan(filter, func(entry journalEntry) {
		if entry.Audit == nil {
			return
		}
		event := *entry.Audit
		if within(event.At, filter) && (filter.Actor == "" || event.Actor == filter.Actor) && (filter.Action == "" || strings.Contains(event.Action, filter.Action)) {
			out = append(out, event)
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	start, end := pageBounds(filter, len(out))
	return out[start:end], len(out), err
}

type Summary struct {
	Consumer            string `json:"consumer,omitempty"`
	Owner               string `json:"owner,omitempty"`
	Channel             string `json:"channel,omitempty"`
	Model               string `json:"model,omitempty"`
	Day                 string `json:"day,omitempty"`
	Calls               int64  `json:"calls"`
	Executions          int64  `json:"executions"`
	Failures            int64  `json:"failures"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
	UnknownTokenRecords int64  `json:"unknown_token_records"`
}

func (s *Store) Summary(filter Filter) (Summary, []Summary, error) {
	var total Summary
	groups := map[string]*Summary{}
	seen := map[string]bool{}
	err := s.scan(filter, func(entry journalEntry) {
		if entry.Usage == nil || !matchUsage(*entry.Usage, filter) {
			return
		}
		u := entry.Usage
		groupKey := u.At.UTC().Format("2006-01-02") + "|" + u.Consumer + "|" + u.Owner + "|" + u.Channel + "|" + u.Model
		group := groups[groupKey]
		if group == nil {
			group = &Summary{Day: u.At.UTC().Format("2006-01-02"), Consumer: u.Consumer, Owner: u.Owner, Channel: u.Channel, Model: u.Model}
			groups[groupKey] = group
		}
		for _, sum := range []*Summary{&total, group} {
			sum.Executions++
			if u.Failed {
				sum.Failures++
			}
			if !u.TokensKnown {
				sum.UnknownTokenRecords++
			}
			sum.InputTokens += u.InputTokens
			sum.OutputTokens += u.OutputTokens
			sum.ReasoningTokens += u.ReasoningTokens
			sum.CacheReadTokens += u.CacheReadTokens
			sum.CacheCreationTokens += u.CacheCreationTokens
			sum.TotalTokens += u.TotalTokens
		}
		trace := u.TraceID
		if trace == "" {
			trace = u.RequestID
		}
		if trace == "" {
			trace = u.ID
		}
		if !seen[trace] {
			total.Calls++
			seen[trace] = true
		}
		if !seen[groupKey+"|"+trace] {
			group.Calls++
			seen[groupKey+"|"+trace] = true
		}
	})
	out := make([]Summary, 0, len(groups))
	for _, sum := range groups {
		out = append(out, *sum)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day > out[j].Day
		}
		return out[i].Channel+out[i].Consumer+out[i].Model < out[j].Channel+out[j].Consumer+out[j].Model
	})
	return total, out, err
}

// PerformanceBucket holds raw aggregates for one hour of usage. Rates and
// averages are derived by callers so buckets can be merged across routes.
type PerformanceBucket struct {
	Start        int64 `json:"start"`
	Requests     int64 `json:"requests"`
	Failures     int64 `json:"failures"`
	LatencyMS    int64 `json:"latency_ms"`
	LatencyCount int64 `json:"latency_count"`
	TTFTMS       int64 `json:"ttft_ms"`
	TTFTCount    int64 `json:"ttft_count"`
	OutputTokens int64 `json:"output_tokens"`
	GenerationMS int64 `json:"generation_ms"`
}

func (b *PerformanceBucket) add(u *UsageEvent) {
	b.Requests++
	if u.Failed {
		b.Failures++
		return
	}
	if u.LatencyMS > 0 {
		b.LatencyMS += u.LatencyMS
		b.LatencyCount++
		generation := u.LatencyMS
		if u.TTFTMS > 0 && u.TTFTMS < u.LatencyMS {
			generation = u.LatencyMS - u.TTFTMS
		}
		b.GenerationMS += generation
		b.OutputTokens += u.OutputTokens
	}
	if u.TTFTMS > 0 {
		b.TTFTMS += u.TTFTMS
		b.TTFTCount++
	}
}

// ModelPerformance aggregates the trailing 24 hours of usage for one pinned
// model ID: totals plus one bucket per hour, oldest first.
type ModelPerformance struct {
	Model   string              `json:"model"`
	Totals  PerformanceBucket   `json:"totals"`
	Buckets []PerformanceBucket `json:"buckets"`
}

const performanceBucketCount = 24

// ModelPerformance scans the journal for usage in the 24 full hourly buckets
// ending at the hour containing now, grouped by pinned model ID.
func (s *Store) ModelPerformance(now time.Time) (map[string]*ModelPerformance, error) {
	end := now.UTC().Truncate(time.Hour).Add(time.Hour)
	start := end.Add(-performanceBucketCount * time.Hour)
	window := Filter{From: start, To: end}
	out := map[string]*ModelPerformance{}
	err := s.scan(window, func(entry journalEntry) {
		if entry.Usage == nil {
			return
		}
		u := entry.Usage
		if u.Model == "" || !within(u.At, window) {
			return
		}
		perf := out[u.Model]
		if perf == nil {
			perf = &ModelPerformance{Model: u.Model, Buckets: make([]PerformanceBucket, performanceBucketCount)}
			for i := range perf.Buckets {
				perf.Buckets[i].Start = start.Add(time.Duration(i) * time.Hour).Unix()
			}
			out[u.Model] = perf
		}
		perf.Totals.add(u)
		if idx := int(u.At.Sub(start) / time.Hour); idx >= 0 && idx < performanceBucketCount {
			perf.Buckets[idx].add(u)
		}
	})
	return out, err
}
