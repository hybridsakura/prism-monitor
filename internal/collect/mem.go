package collect

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// meminfo /proc/meminfo 中我们关心的字段（字节）。
type meminfo struct {
	memTotal, memAvailable, buffers, cached, swapCached, swapTotal, swapFree uint64
}

// parseMeminfo 解析 /proc/meminfo，单位从 kB 换算为字节。
func parseMeminfo(r io.Reader) (meminfo, error) {
	var m meminfo
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		var kb uint64
		if _, err := fmt.Sscanf(fields[1], "%d", &kb); err != nil {
			continue // 非数字行直接跳过
		}
		v := kb * 1024
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			m.memTotal = v
		case "MemAvailable":
			m.memAvailable = v
		case "Buffers":
			m.buffers = v
		case "Cached":
			m.cached = v
		case "SwapCached":
			m.swapCached = v
		case "SwapTotal":
			m.swapTotal = v
		case "SwapFree":
			m.swapFree = v
		}
	}
	if err := sc.Err(); err != nil {
		return m, err
	}
	if m.memTotal == 0 {
		return m, fmt.Errorf("meminfo 中 MemTotal 缺失或为 0")
	}
	return m, nil
}

// memStats 由 meminfo 计算内存与 SWAP 使用量。
func memStats(m meminfo) (MemStat, SwapStat) {
	used := uint64(0)
	if m.memTotal > m.memAvailable {
		used = m.memTotal - m.memAvailable
	}
	mem := MemStat{
		Total:  m.memTotal,
		Used:   used,
		Cached: m.cached + m.buffers,
		Avail:  m.memAvailable,
		Pct:    clampPct(100 * float64(used) / float64(m.memTotal)),
	}
	swapUsed := uint64(0)
	if m.swapTotal > m.swapFree {
		swapUsed = m.swapTotal - m.swapFree
	}
	swap := SwapStat{Total: m.swapTotal, Used: swapUsed, Cached: m.swapCached}
	if m.swapTotal > 0 {
		swap.Pct = clampPct(100 * float64(swapUsed) / float64(m.swapTotal))
	}
	return mem, swap
}

// vmstatCounters /proc/vmstat 中的页面换入换出累计计数。
type vmstatCounters struct {
	pswpin, pswpout uint64
}

func parseVmstat(r io.Reader) (vmstatCounters, error) {
	var c vmstatCounters
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		var v uint64
		if _, err := fmt.Sscanf(fields[1], "%d", &v); err != nil {
			continue
		}
		switch fields[0] {
		case "pswpin":
			c.pswpin = v
		case "pswpout":
			c.pswpout = v
		}
	}
	return c, sc.Err()
}

// pageSize 默认页大小（字节），x86/ARM 通用 4K。
const pageSize = 4096

// swapRates 由 pswpin/pswpout 差量计算换入换出速率（字节/s）。
func swapRates(prev, cur vmstatCounters, dt float64) (inBps, outBps float64) {
	if dt <= 0 || cur.pswpin < prev.pswpin || cur.pswpout < prev.pswpout {
		return 0, 0
	}
	return float64(cur.pswpin-prev.pswpin) * pageSize / dt,
		float64(cur.pswpout-prev.pswpout) * pageSize / dt
}
