// Package collect 从 /proc 与 /sys 采集服务器资源指标。
// 所有解析函数均为纯函数（io.Reader 进、值对象出），
// 速率/百分比等差量换算集中在 Collector 内完成，便于 TDD。
package collect

// Snapshot 一次采集的完整指标快照（纯值对象，可直接 JSON 序列化）。
type Snapshot struct {
	Ts     int64       `json:"ts"`
	Host   HostStat    `json:"host"`
	Uptime int64       `json:"uptime"`
	CPU    CPUStat     `json:"cpu"`
	Mem    MemStat     `json:"mem"`
	Swap   SwapStat    `json:"swap"`
	Zram   []ZramStat  `json:"zram"`
	Disks  []DiskStat  `json:"disks"`
	Mounts []MountStat `json:"mounts"`
	Nets   []NetStat   `json:"nets"`
}

type HostStat struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
}

type CPUStat struct {
	Total  float64    `json:"total"`  // 总使用率 %
	Steal  float64    `json:"steal"`  // steal 时间占比 %（虚拟机被宿主机抢占）
	Iowait float64    `json:"iowait"` // iowait 占比 %
	Cores  []float64  `json:"cores"`  // 每核心使用率 %
	Load   [3]float64 `json:"load"`   // 1/5/15 分钟负载
}

type MemStat struct {
	Total  uint64  `json:"total"`  // 字节
	Used   uint64  `json:"used"`   // total - available
	Cached uint64  `json:"cached"` // page cache（含 buffers）
	Avail  uint64  `json:"avail"`
	Pct    float64 `json:"pct"`
}

type SwapStat struct {
	Total  uint64  `json:"total"`
	Used   uint64  `json:"used"`
	Pct    float64 `json:"pct"`
	InBps  float64 `json:"in_bps"`  // 换入速率 字节/s
	OutBps float64 `json:"out_bps"` // 换出速率 字节/s
	Cached uint64  `json:"cached"`  // SwapCached 字节
}

type ZramStat struct {
	Dev   string  `json:"dev"`   // "zram0"
	Algo  string  `json:"algo"`  // 当前压缩算法，如 "zstd"
	Used  uint64  `json:"used"`  // mem_used_total 字节
	Total uint64  `json:"total"` // disksize 字节
	Orig  uint64  `json:"orig"`  // 原始数据量 字节
	Ratio float64 `json:"ratio"` // 压缩率 orig/compr
}

type DiskStat struct {
	Dev       string  `json:"dev"`
	ReadBps   float64 `json:"read_bps"`
	WriteBps  float64 `json:"write_bps"`
	ReadIops  float64 `json:"read_iops"`
	WriteIops float64 `json:"write_iops"`
	AwaitMs   float64 `json:"await_ms"`
}

type MountStat struct {
	Path  string  `json:"path"`
	Dev   string  `json:"dev"`
	Fs    string  `json:"fs"`
	Total uint64  `json:"total"`
	Used  uint64  `json:"used"`
	Pct   float64 `json:"pct"`
}

type NetStat struct {
	If    string  `json:"if"`
	RxBps float64 `json:"rx_bps"`
	TxBps float64 `json:"tx_bps"`
}

// Metrics 把快照摊平成时序指标名 → 值，供历史存储落盘。
// 维度编码进指标名：disk.sda.read_bps / net.eth0.rx_bps / mount._data.pct。
func (s *Snapshot) Metrics() map[string]float64 {
	m := map[string]float64{
		"cpu.total":     s.CPU.Total,
		"cpu.steal":     s.CPU.Steal,
		"cpu.iowait":    s.CPU.Iowait,
		"cpu.load1":     s.CPU.Load[0],
		"mem.pct":       s.Mem.Pct,
		"mem.used":      float64(s.Mem.Used),
		"swap.pct":      s.Swap.Pct,
		"swap.in_bps":   s.Swap.InBps,
		"swap.out_bps":  s.Swap.OutBps,
		"swap.used":     float64(s.Swap.Used),
	}
	for _, d := range s.Disks {
		m["disk."+d.Dev+".read_bps"] = d.ReadBps
		m["disk."+d.Dev+".write_bps"] = d.WriteBps
		m["disk."+d.Dev+".await_ms"] = d.AwaitMs
	}
	for _, n := range s.Nets {
		m["net."+n.If+".rx_bps"] = n.RxBps
		m["net."+n.If+".tx_bps"] = n.TxBps
	}
	for _, mt := range s.Mounts {
		m["mount."+EscapePath(mt.Path)+".pct"] = mt.Pct
	}
	for _, z := range s.Zram {
		m["zram."+z.Dev+".used"] = float64(z.Used)
		m["zram."+z.Dev+".ratio"] = z.Ratio
	}
	return m
}

// EscapePath 把挂载点路径编码成指标名片段："/" → "_root"，"/data" → "_data"。
func EscapePath(p string) string {
	if p == "/" {
		return "_root"
	}
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '/' {
			out = append(out, '_')
		} else {
			out = append(out, c)
		}
	}
	return string(out)
}
