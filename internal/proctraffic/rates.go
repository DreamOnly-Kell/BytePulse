// Rate helpers for Windows TCP ESTATS (and any cumulative-byte backends).
// Windows TCP ESTATS（及任何累计字节后端）的速率计算辅助。
package proctraffic

import "time"

// pidByteCounters holds cumulative TCP byte totals for one process.
// pidByteCounters 保存某进程的 TCP 累计字节总量。
type pidByteCounters struct {
	RX uint64
	TX uint64
}

// computeRateSamples turns successive cumulative counters into per-second samples.
// computeRateSamples 将相邻两次累计计数转换为每秒速率样本。
//
// curr is absolute byte totals (not deltas). Rates use max(0, curr-prev)/dt.
// curr 为绝对累计字节（非增量）。速率用 max(0, curr-prev)/dt。
func computeRateSamples(prev, curr map[int]pidByteCounters, dtSec float64, seenAt time.Time, source string, meta map[int]processMeta) []Sample {
	if dtSec <= 0 {
		dtSec = 1
	}
	out := make([]Sample, 0, len(curr))
	for pid, c := range curr {
		var rxDelta, txDelta uint64
		if p, ok := prev[pid]; ok {
			if c.RX >= p.RX {
				rxDelta = c.RX - p.RX
			}
			if c.TX >= p.TX {
				txDelta = c.TX - p.TX
			}
		}
		// First observation for a PID: no reliable rate yet (skip or zero).
		// 某 PID 首次出现：尚无可靠速率（跳过）。
		if _, ok := prev[pid]; !ok {
			continue
		}
		m := meta[pid]
		out = append(out, Sample{
			PID:         pid,
			ProcessName: m.name,
			ProcessPath: m.path,
			RXBytes:     rxDelta,
			TXBytes:     txDelta,
			RXBps:       float64(rxDelta) / dtSec,
			TXBps:       float64(txDelta) / dtSec,
			SeenAt:      seenAt,
			Source:      source,
		})
	}
	return out
}

// processMeta is optional name/path attached when emitting samples.
// processMeta 是产出样本时可选附带的名称/路径。
type processMeta struct {
	name string
	path string
}
