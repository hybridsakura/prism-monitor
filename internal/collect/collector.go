package collect

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Collector 持态采集器：跨两次采样保存累计计数，负责所有差量换算。
// root 为 /proc、/sys 的前缀（容器内挂宿主时可指到 /host），默认 "/"。
type Collector struct {
	root string

	hostname string

	mu        sync.Mutex
	hasPrev   bool
	prevTime  time.Time
	prevAgg   cpuTimes
	prevCores []cpuTimes
	prevVM    vmstatCounters
	prevDisks map[string]diskCounters
	prevNets  map[string]netCounters
}

// New 创建采集器。root 传 "" 或 "/" 表示真实系统根。
func New(root string) *Collector {
	h, err := os.Hostname()
	if err != nil {
		h = "unknown"
	}
	return &Collector{root: root, hostname: h}
}

func (c *Collector) path(rel string) string {
	if c.root == "" || c.root == "/" {
		return "/" + rel
	}
	return filepath.Join(c.root, rel)
}

// Collect 采集一次完整快照。采集错误按维度降级（该维度留空），不整体失败。
func (c *Collector) Collect() (*Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	snap := &Snapshot{Ts: now.Unix()}
	snap.Host.Hostname = c.hostname
	snap.Host.OS = c.readOSRelease()

	// CPU
	if agg, cores, err := c.readProcStat(); err == nil {
		var total, steal, iowait float64
		var corePcts []float64
		if c.hasPrev {
			dt := now.Sub(c.prevTime).Seconds()
			_ = dt // cpu 百分比用 jiffies 差量，与墙钟无关
			total, steal, iowait = cpuPercents(c.prevAgg, agg)
			corePcts = make([]float64, 0, len(cores))
			for i, core := range cores {
				var prev cpuTimes
				if i < len(c.prevCores) {
					prev = c.prevCores[i]
				}
				p, _, _ := cpuPercents(prev, core)
				corePcts = append(corePcts, p)
			}
		} else {
			corePcts = make([]float64, len(cores))
		}
		snap.CPU = CPUStat{Total: total, Steal: steal, Iowait: iowait, Cores: corePcts}
		c.prevAgg, c.prevCores = agg, cores
	}
	if load, err := c.readLoadavg(); err == nil {
		snap.CPU.Load = load
	}

	// 内存 + SWAP 用量
	if m, err := c.readMeminfo(); err == nil {
		snap.Mem, snap.Swap = memStats(m)
	}

	// SWAP 换入换出速率
	if vm, err := c.readVmstat(); err == nil {
		if c.hasPrev {
			dt := now.Sub(c.prevTime).Seconds()
			snap.Swap.InBps, snap.Swap.OutBps = swapRates(c.prevVM, vm, dt)
		}
		c.prevVM = vm
	}

	// 磁盘 IO
	if disks, err := c.readDiskstats(); err == nil {
		dt := now.Sub(c.prevTime).Seconds()
		for _, d := range disks {
			var prev diskCounters
			if c.hasPrev {
				prev = c.prevDisks[d.dev]
			}
			snap.Disks = append(snap.Disks, diskRates(prev, d, dt))
		}
		c.prevDisks = indexDisks(disks)
	}

	// 网络
	if nets, err := c.readNetDev(); err == nil {
		dt := now.Sub(c.prevTime).Seconds()
		for _, n := range nets {
			var prev netCounters
			if c.hasPrev {
				prev = c.prevNets[n.iface]
			}
			snap.Nets = append(snap.Nets, netRates(prev, n, dt))
		}
		c.prevNets = indexNets(nets)
	}

	// 挂载点占用
	if entries, err := c.readMounts(); err == nil {
		for _, e := range entries {
			if ms, ok := mountStat(e); ok {
				snap.Mounts = append(snap.Mounts, ms)
			}
		}
	}

	// ZRAM
	snap.Zram = c.readZram()

	// 运行时长
	if up, err := c.readUptime(); err == nil {
		snap.Uptime = up
	}

	c.prevTime = now
	c.hasPrev = true
	return snap, nil
}

func indexDisks(ds []diskCounters) map[string]diskCounters {
	m := make(map[string]diskCounters, len(ds))
	for _, d := range ds {
		m[d.dev] = d
	}
	return m
}

func indexNets(ns []netCounters) map[string]netCounters {
	m := make(map[string]netCounters, len(ns))
	for _, n := range ns {
		m[n.iface] = n
	}
	return m
}

func (c *Collector) readProcStat() (cpuTimes, []cpuTimes, error) {
	f, err := os.Open(c.path("proc/stat"))
	if err != nil {
		return cpuTimes{}, nil, err
	}
	defer f.Close()
	return parseProcStat(f)
}

func (c *Collector) readLoadavg() ([3]float64, error) {
	f, err := os.Open(c.path("proc/loadavg"))
	if err != nil {
		return [3]float64{}, err
	}
	defer f.Close()
	return parseLoadavg(f)
}

func (c *Collector) readMeminfo() (meminfo, error) {
	f, err := os.Open(c.path("proc/meminfo"))
	if err != nil {
		return meminfo{}, err
	}
	defer f.Close()
	return parseMeminfo(f)
}

func (c *Collector) readVmstat() (vmstatCounters, error) {
	f, err := os.Open(c.path("proc/vmstat"))
	if err != nil {
		return vmstatCounters{}, err
	}
	defer f.Close()
	return parseVmstat(f)
}

func (c *Collector) readDiskstats() ([]diskCounters, error) {
	f, err := os.Open(c.path("proc/diskstats"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseDiskstats(f)
}

func (c *Collector) readNetDev() ([]netCounters, error) {
	f, err := os.Open(c.path("proc/net/dev"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseNetDev(f)
}

func (c *Collector) readMounts() ([]mountEntry, error) {
	f, err := os.Open(c.path("proc/mounts"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseMounts(f), nil
}

func (c *Collector) readUptime() (int64, error) {
	b, err := os.ReadFile(c.path("proc/uptime"))
	if err != nil {
		return 0, err
	}
	var up float64
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, fmt.Errorf("proc/uptime 为空")
	}
	if _, err := fmt.Sscanf(fields[0], "%g", &up); err != nil {
		return 0, err
	}
	return int64(up), nil
}

// readZram 扫描 sys/block/zram*，未启用 ZRAM 时返回空。
func (c *Collector) readZram() []ZramStat {
	dirs, err := filepath.Glob(c.path("sys/block/zram*"))
	if err != nil {
		return nil
	}
	var out []ZramStat
	for _, dir := range dirs {
		dev := filepath.Base(dir)
		mm, err := readFileParse(dir, "mm_stat", parseMMStat)
		if err != nil {
			continue
		}
		var disksize uint64
		if b, err := os.ReadFile(filepath.Join(dir, "disksize")); err == nil {
			_, _ = fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &disksize)
		}
		algo := ""
		if f, err := os.Open(filepath.Join(dir, "comp_algorithm")); err == nil {
			algo = parseCompAlgorithm(f)
			f.Close()
		}
		out = append(out, zramStats(dev, mm, disksize, algo))
	}
	return out
}

func readFileParse[T any](dir, name string, fn func(r io.Reader) (T, error)) (T, error) {
	var zero T
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return zero, err
	}
	defer f.Close()
	return fn(f)
}

// readOSRelease 读 etc/os-release 的 PRETTY_NAME。
func (c *Collector) readOSRelease() string {
	b, err := os.ReadFile(c.path("etc/os-release"))
	if err != nil {
		return "Linux"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			v := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(v, `"`)
		}
	}
	return "Linux"
}
