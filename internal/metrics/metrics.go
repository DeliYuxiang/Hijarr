// Package metrics provides lightweight atomic counters and a rolling-window
// report suitable for periodic INFO-level summaries.
package metrics

import (
	"fmt"
	"sync/atomic"
	"time"
)

var (
	SRNQueryTotal atomic.Int64 // every SearchByCacheKey call
	SRNQueryHit   atomic.Int64 // returned ≥1 result (local / backend / relay)
)

// StatsJSON holds all current metrics for API consumption.
type StatsJSON struct {
	SRNQueryTotal int64 `json:"srn_query_total"`
	SRNQueryHit   int64 `json:"srn_query_hit"`
}

// CurrentJSON returns a JSON-friendly struct of current counters.
func CurrentJSON() StatsJSON {
	return StatsJSON{
		SRNQueryTotal: SRNQueryTotal.Load(),
		SRNQueryHit:   SRNQueryHit.Load(),
	}
}

var (
	lastTotal int64
	lastHit   int64
	lastAt    time.Time
)

// Report computes a delta since the last call, formats a summary string,
// and advances the internal snapshot.  Thread-safe to call from a single
// goroutine (the periodic reporter).
func Report() string {
	now := time.Now()
	total := SRNQueryTotal.Load()
	hit := SRNQueryHit.Load()

	dTotal := total - lastTotal
	dHit := hit - lastHit

	hitRate := "N/A"
	if dTotal > 0 {
		hitRate = fmt.Sprintf("%.1f%%", float64(dHit)*100/float64(dTotal))
	}

	window := "启动以来"
	if !lastAt.IsZero() {
		window = fmt.Sprintf("最近 %v", now.Sub(lastAt).Round(time.Minute))
	}

	lastTotal, lastHit, lastAt = total, hit, now

	return fmt.Sprintf(
		"📈 [统计 %s]\n   SRN查询  %d 次 | 命中率 %s",
		window, dTotal, hitRate,
	)
}
