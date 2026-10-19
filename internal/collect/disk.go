package collect

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// diskCounters /proc/diskstats 单行中我们关心的累计值。
type diskCounters struct {
	dev         string
	readsDone   uint64 // 字段 3: reads completed
	sectorsRead uint64 // 字段 5
	msReading   uint64 // 字段 6
	writesDone  uint64 // 字段 7
	sectorsWr   uint64 // 字段 9
	msWriting   uint64 // 字段 10
}

const sectorSize = 512

// skipDisk 虚拟设备过滤：loop/ram/sr/zram/fd 不展示（zram 有独立面板）。
func skipDisk(dev string) bool {
	for _, p := range []string{"loop", "ram", "sr", "zram", "fd"} {
		if strings.HasPrefix(dev, p) {
			return true
		}
	}
	return false
}

// parseDiskstats 解析 /proc/diskstats。
func parseDiskstats(r io.Reader) ([]diskCounters, error) {
	var out []diskCounters
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		dev := fields[2]
		if skipDisk(dev) {
			continue
		}
		var c diskCounters
		c.dev = dev
		vals := []*uint64{&c.readsDone, nil, &c.sectorsRead, &c.msReading, &c.writesDone, nil, &c.sectorsWr, &c.msWriting}
		// fields[3..10] 依次是 readsDone, readsMerged, sectorsRead, msReading, writesDone, writesMerged, sectorsWr, msWriting
		ok := true
		for i, ptr := range vals {
			if ptr == nil {
				continue
			}
			if _, err := fmt.Sscanf(fields[3+i], "%d", ptr); err != nil {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	return out, sc.Err()
}

// diskRates 由前后两次累计值计算速率/IOPS/平均延迟。
// 首次采样或计数器回绕时返回零值。
func diskRates(prev, cur diskCounters, dt float64) DiskStat {
	d := DiskStat{Dev: cur.dev}
	if dt <= 0 || cur.readsDone < prev.readsDone || cur.writesDone < prev.writesDone {
		return d
	}
	d.ReadBps = float64(cur.sectorsRead-prev.sectorsRead) * sectorSize / dt
	d.WriteBps = float64(cur.sectorsWr-prev.sectorsWr) * sectorSize / dt
	d.ReadIops = float64(cur.readsDone-prev.readsDone) / dt
	d.WriteIops = float64(cur.writesDone-prev.writesDone) / dt
	ios := float64(cur.readsDone-prev.readsDone + cur.writesDone - prev.writesDone)
	if ios > 0 {
		d.AwaitMs = float64(cur.msReading-prev.msReading+cur.msWriting-prev.msWriting) / ios
	}
	return d
}

// parseNetDev 解析 /proc/net/dev，跳过 lo。
func parseNetDev(r io.Reader) ([]netCounters, error) {
	var out []netCounters
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:idx])
		if iface == "lo" || iface == "" {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			continue
		}
		var c netCounters
		c.iface = iface
		if _, err := fmt.Sscanf(fields[0], "%d", &c.rxBytes); err != nil {
			continue
		}
		if _, err := fmt.Sscanf(fields[8], "%d", &c.txBytes); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

type netCounters struct {
	iface          string
	rxBytes, txBytes uint64
}

// netRates 计算网卡收发速率（字节/s）。
func netRates(prev, cur netCounters, dt float64) NetStat {
	n := NetStat{If: cur.iface}
	if dt <= 0 || cur.rxBytes < prev.rxBytes || cur.txBytes < prev.txBytes {
		return n
	}
	n.RxBps = float64(cur.rxBytes-prev.rxBytes) / dt
	n.TxBps = float64(cur.txBytes-prev.txBytes) / dt
	return n
}
