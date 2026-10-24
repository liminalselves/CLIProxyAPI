package multiuser

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type journalCacheEntry struct {
	value  any
	weight int
}

type journalFlight struct {
	done   chan struct{}
	cancel context.CancelFunc
	users  int
	value  any
	err    error
}

// aggregate shares identical reads and cancels the SQL work when its last reader leaves.
func (j *journalIndex) aggregate(ctx context.Context, key string, compute func(context.Context) (any, int, error)) (any, error) {
	j.cacheMu.Lock()
	key = strconv.FormatUint(j.version, 10) + ":" + key
	if entry, ok := j.cache[key]; ok {
		j.cacheMu.Unlock()
		return entry.value, nil
	}
	flight := j.flights[key]
	if flight == nil {
		shared, cancel := context.WithCancel(j.ctx)
		flight = &journalFlight{done: make(chan struct{}), cancel: cancel}
		j.flights[key] = flight
		go func() {
			value, weight, err := compute(shared)
			j.cacheMu.Lock()
			flight.value, flight.err = value, err
			if err == nil && shared.Err() == nil && weight <= 32768 && strings.HasPrefix(key, strconv.FormatUint(j.version, 10)+":") {
				if len(j.cache) >= 32 || j.weight+weight > 32768 {
					j.cache = map[string]journalCacheEntry{}
					j.weight = 0
				}
				j.cache[key] = journalCacheEntry{value: value, weight: weight}
				j.weight += weight
			}
			if j.flights[key] == flight {
				delete(j.flights, key)
			}
			close(flight.done)
			j.cacheMu.Unlock()
			cancel()
		}()
	}
	flight.users++
	j.cacheMu.Unlock()
	defer func() {
		j.cacheMu.Lock()
		flight.users--
		if flight.users == 0 {
			flight.cancel()
			if j.flights[key] == flight {
				delete(j.flights, key)
			}
		}
		j.cacheMu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.value, flight.err
	}
}

func journalWhere(kind int, f Filter) (string, []any) {
	clauses := []string{fmt.Sprintf("kind=%d", kind)}
	args := []any{}
	add := func(clause string, value any) {
		clauses = append(clauses, clause)
		args = append(args, value)
	}
	if !f.From.IsZero() {
		add("at>=?", f.From.UTC().Format(journalTimeFormat))
	}
	if !f.To.IsZero() {
		add("at<?", f.To.UTC().Format(journalTimeFormat))
	}
	if kind == 1 || kind == 2 {
		for _, field := range []struct{ name, value string }{{"consumer", f.Consumer}, {"key_id", f.KeyID}} {
			if field.value != "" {
				add(field.name+"=?", field.value)
			}
		}
	}
	if kind == 1 {
		for _, field := range []struct{ name, value string }{{"owner", f.Owner}, {"channel_id", f.ChannelID}, {"credential_id", f.CredentialID}} {
			if field.value != "" {
				add(field.name+"=?", field.value)
			}
		}
		q := strings.ToLower(strings.TrimSpace(f.Model))
		if pin, ok := modelQueryPin(q); ok {
			clauses = append(clauses, "(serving_lower=? OR model_lower=?)")
			args = append(args, pin, pin)
		} else if q != "" {
			add("instr(model_lower,?)>0", q)
		}
	} else if kind == 2 && f.Model != "" {
		add("instr(model,?)>0", f.Model)
	} else if kind == 3 {
		if f.Actor != "" {
			add("actor=?", f.Actor)
		}
		if f.Action != "" {
			add("instr(action,?)>0", f.Action)
		}
	}
	return strings.Join(clauses, " AND "), args
}

func journalPage[T any](ctx context.Context, j *journalIndex, kind int, filter Filter, event func(journalEntry) *T) ([]T, int, error) {
	where, args := journalWhere(kind, filter)
	tx, err := j.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE "+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	start, end := pageBounds(filter, count)
	args = append(args, end-start, start)
	rows, err := tx.QueryContext(ctx, "SELECT source, position, length FROM events WHERE "+where+" ORDER BY at DESC, source, position LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	files := map[string]*os.File{}
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	out := make([]T, 0, end-start)
	for rows.Next() {
		var source string
		var offset int64
		var length int
		if err = rows.Scan(&source, &offset, &length); err != nil {
			return nil, 0, err
		}
		if err = ctx.Err(); err != nil {
			return nil, 0, err
		}
		file := files[source]
		if file == nil {
			file, err = os.Open(filepath.Join(j.store.dir, "journal", source))
			if err != nil {
				return nil, 0, err
			}
			files[source] = file
		}
		data := make([]byte, length)
		if _, err = file.ReadAt(data, offset); err != nil && err != io.EOF {
			return nil, 0, err
		}
		var entry journalEntry
		if err = json.Unmarshal(data, &entry); err != nil {
			return nil, 0, fmt.Errorf("read indexed journal %s at byte %d: %w", source, offset, err)
		}
		item := event(entry)
		if item == nil {
			return nil, 0, fmt.Errorf("journal %s changed at byte %d", source, offset)
		}
		out = append(out, *item)
	}
	return out, count, rows.Err()
}

func (s *Store) UsageContext(ctx context.Context, filter Filter) ([]UsageEvent, int, error) {
	j, err := s.queryJournal(ctx)
	if err != nil {
		return nil, 0, err
	}
	return journalPage(ctx, j, 1, filter, func(e journalEntry) *UsageEvent { return e.Usage })
}

func (s *Store) CallsContext(ctx context.Context, filter Filter) ([]CallEvent, int, error) {
	j, err := s.queryJournal(ctx)
	if err != nil {
		return nil, 0, err
	}
	return journalPage(ctx, j, 2, filter, func(e journalEntry) *CallEvent { return e.Call })
}

func (s *Store) AuditsContext(ctx context.Context, filter Filter) ([]AuditEvent, int, error) {
	j, err := s.queryJournal(ctx)
	if err != nil {
		return nil, 0, err
	}
	return journalPage(ctx, j, 3, filter, func(e journalEntry) *AuditEvent { return e.Audit })
}

type summaryResult struct {
	total  Summary
	groups []Summary
}

const summaryColumns = `COUNT(DISTINCT trace), COUNT(*), COALESCE(SUM(failed),0),
 COALESCE(SUM(input),0), COALESCE(SUM(output),0), COALESCE(SUM(reasoning),0),
 COALESCE(SUM(cache_read),0), COALESCE(SUM(cache_create),0), COALESCE(SUM(total),0), COALESCE(SUM(1-known),0)`

func summaryDest(sum *Summary) []any {
	return []any{&sum.Calls, &sum.Executions, &sum.Failures, &sum.InputTokens, &sum.OutputTokens, &sum.ReasoningTokens, &sum.CacheReadTokens, &sum.CacheCreationTokens, &sum.TotalTokens, &sum.UnknownTokenRecords}
}

func (s *Store) SummaryContext(ctx context.Context, filter Filter) (Summary, []Summary, error) {
	j, err := s.queryJournal(ctx)
	if err != nil {
		return Summary{}, nil, err
	}
	filter.Page, filter.PageSize = 0, 0
	key, err := json.Marshal(filter)
	if err != nil {
		return Summary{}, nil, err
	}
	value, err := j.aggregate(ctx, "summary:"+string(key), func(ctx context.Context) (any, int, error) {
		result := summaryResult{groups: []Summary{}}
		where, args := journalWhere(1, filter)
		// The covering index avoids a random table read per event, even when
		// an account or time filter happens to match nearly the whole history.
		from := "events INDEXED BY usage_summary"
		tx, errBegin := j.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if errBegin != nil {
			return nil, 0, errBegin
		}
		defer func() { _ = tx.Rollback() }()
		traceFrom := from
		if where == "kind=1" {
			traceFrom = "events INDEXED BY usage_trace"
		} else if filter.Consumer != "" && filter == (Filter{Consumer: filter.Consumer}) {
			traceFrom = "events INDEXED BY usage_consumer_trace"
		} else if filter.Owner != "" && filter == (Filter{Owner: filter.Owner}) {
			traceFrom = "events INDEXED BY usage_owner_trace"
		}
		if errTotal := tx.QueryRowContext(ctx, "SELECT COUNT(DISTINCT trace) FROM "+traceFrom+" WHERE "+where, args...).Scan(&result.total.Calls); errTotal != nil {
			return nil, 0, errTotal
		}
		rows, errQuery := tx.QueryContext(ctx, "SELECT day, consumer, owner, channel, model, "+summaryColumns+" FROM "+from+" WHERE "+where+" GROUP BY day,consumer,owner,channel,model ORDER BY day DESC,channel||consumer||model", args...)
		if errQuery != nil {
			return nil, 0, errQuery
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var group Summary
			dest := append([]any{&group.Day, &group.Consumer, &group.Owner, &group.Channel, &group.Model}, summaryDest(&group)...)
			if errScan := rows.Scan(dest...); errScan != nil {
				return nil, 0, errScan
			}
			result.groups = append(result.groups, group)
			result.total.Executions += group.Executions
			result.total.Failures += group.Failures
			result.total.InputTokens += group.InputTokens
			result.total.OutputTokens += group.OutputTokens
			result.total.ReasoningTokens += group.ReasoningTokens
			result.total.CacheReadTokens += group.CacheReadTokens
			result.total.CacheCreationTokens += group.CacheCreationTokens
			result.total.TotalTokens += group.TotalTokens
			result.total.UnknownTokenRecords += group.UnknownTokenRecords
		}
		return result, len(result.groups) + 1, rows.Err()
	})
	if err != nil {
		return Summary{}, nil, err
	}
	result := value.(summaryResult)
	return result.total, append([]Summary{}, result.groups...), nil
}

func (s *Store) ModelPerformanceContext(ctx context.Context, now time.Time) (map[string]*ModelPerformance, error) {
	j, err := s.queryJournal(ctx)
	if err != nil {
		return nil, err
	}
	end := now.UTC().Truncate(time.Hour).Add(time.Hour)
	start := end.Add(-performanceBucketCount * time.Hour)
	value, err := j.aggregate(ctx, "performance:"+end.Format(journalTimeFormat), func(ctx context.Context) (any, int, error) {
		rows, errQuery := j.db.QueryContext(ctx, `SELECT model, hour, COUNT(*), SUM(failed),
 COALESCE(SUM(CASE WHEN failed=0 AND latency>0 THEN latency ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN failed=0 AND latency>0 THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN failed=0 AND ttft>0 THEN ttft ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN failed=0 AND ttft>0 THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN failed=0 AND latency>0 THEN output ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN failed=0 AND latency>0 THEN latency-CASE WHEN ttft>0 AND ttft<latency THEN ttft ELSE 0 END ELSE 0 END),0)
 FROM events INDEXED BY usage_performance WHERE kind=1 AND hour>=? AND hour<? AND model<>'' GROUP BY hour,model`, start.Unix(), end.Unix())
		if errQuery != nil {
			return nil, 0, errQuery
		}
		defer func() { _ = rows.Close() }()
		out := map[string]*ModelPerformance{}
		for rows.Next() {
			var model string
			var bucket PerformanceBucket
			if errScan := rows.Scan(&model, &bucket.Start, &bucket.Requests, &bucket.Failures, &bucket.LatencyMS, &bucket.LatencyCount, &bucket.TTFTMS, &bucket.TTFTCount, &bucket.OutputTokens, &bucket.GenerationMS); errScan != nil {
				return nil, 0, errScan
			}
			perf := out[model]
			if perf == nil {
				perf = &ModelPerformance{Model: model, Buckets: make([]PerformanceBucket, performanceBucketCount)}
				for i := range perf.Buckets {
					perf.Buckets[i].Start = start.Add(time.Duration(i) * time.Hour).Unix()
				}
				out[model] = perf
			}
			index := int((bucket.Start - start.Unix()) / 3600)
			if index >= 0 && index < performanceBucketCount {
				perf.Buckets[index] = bucket
			}
			perf.Totals.Requests += bucket.Requests
			perf.Totals.Failures += bucket.Failures
			perf.Totals.LatencyMS += bucket.LatencyMS
			perf.Totals.LatencyCount += bucket.LatencyCount
			perf.Totals.TTFTMS += bucket.TTFTMS
			perf.Totals.TTFTCount += bucket.TTFTCount
			perf.Totals.OutputTokens += bucket.OutputTokens
			perf.Totals.GenerationMS += bucket.GenerationMS
		}
		return out, len(out) * (performanceBucketCount + 1), rows.Err()
	})
	if err != nil {
		return nil, err
	}
	out := map[string]*ModelPerformance{}
	for model, perf := range value.(map[string]*ModelPerformance) {
		clone := *perf
		clone.Buckets = append([]PerformanceBucket{}, perf.Buckets...)
		out[model] = &clone
	}
	return out, nil
}
