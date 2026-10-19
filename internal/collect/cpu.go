package collect

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// cpuTimes /proc/stat 中一行的各时间片（单位 USER_HZ jiffies）。
type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal float64
}

func (t cpuTimes) total() float64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
}

func (t cpuTimes) idleAll() float64 { return t.idle + t.iowait }

// parseProcStat 解析 /proc/stat，返回聚合行与各核心行。
func parseProcStat(r io.Reader) (cpuTimes, []cpuTimes, error) {
	var agg cpuTimes
	var cores []cpuTimes
	found := false
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] == "cpu" {
			t, err := parseCPULine(fields[1:])
			if err != nil {
				return agg, nil, err
			}
			agg, found = t, true
			continue
		}
		// cpu0 / cpu1 ...；注意排除 "cpu" 聚合行已在上面处理
		t, err := parseCPULine(fields[1:])
		if err != nil {
			return agg, nil, fmt.Errorf("解析 %s 失败: %w", fields[0], err)
		}
		cores = append(cores, t)
	}
	if err := sc.Err(); err != nil {
		return agg, nil, err
	}
	if !found {
		return agg, nil, fmt.Errorf("/proc/stat 中未找到 cpu 聚合行")
	}
	return agg, cores, nil
}

func parseCPULine(fields []string) (cpuTimes, error) {
	var t cpuTimes
	// 至少需要 user nice system idle；iowait/irq/softirq/steal 在老内核可能缺失
	vals := make([]float64, 8)
	for i := 0; i < len(fields) && i < 8; i++ {
		var v float64
		if _, err := fmt.Sscanf(fields[i], "%g", &v); err != nil {
			return t, fmt.Errorf("字段 %q 非数字", fields[i])
		}
		vals[i] = v
	}
	t = cpuTimes{vals[0], vals[1], vals[2], vals[3], vals[4], vals[5], vals[6], vals[7]}
	return t, nil
}

// cpuPercents 由前后两次 jiffies 差量计算使用率/steal/iowait 百分比。
// prev 为零值（首次采样）时返回全 0。
func cpuPercents(prev, cur cpuTimes) (total, steal, iowait float64) {
	totalD := cur.total() - prev.total()
	if prev == (cpuTimes{}) || totalD <= 0 {
		return 0, 0, 0
	}
	idleD := cur.idleAll() - prev.idleAll()
	stealD := cur.steal - prev.steal
	iowaitD := cur.iowait - prev.iowait
	usage := 100 * (totalD - idleD) / totalD
	return clampPct(usage), clampPct(100 * stealD / totalD), clampPct(100 * iowaitD / totalD)
}

// parseLoadavg 解析 /proc/loadavg 前三个字段。
func parseLoadavg(r io.Reader) ([3]float64, error) {
	var l [3]float64
	var rest string
	n, err := fmt.Fscanf(r, "%g %g %g %s", &l[0], &l[1], &l[2], &rest)
	if err != nil && n < 3 {
		return l, fmt.Errorf("解析 loadavg 失败: %w", err)
	}
	return l, nil
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
