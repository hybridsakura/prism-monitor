package collect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GPUStat 显卡/集成显卡指标
type GPUStat struct {
	Model     string  `json:"model"`      // 显卡名称，如 "Radeon HD 8470D"
	Driver    string  `json:"driver"`     // 驱动名，如 "radeon", "amdgpu", "i915", "nvidia"
	FreqMHz   float64 `json:"freq_mhz"`   // 当前核心频率 (MHz)
	TempC     float64 `json:"temp_c"`     // 核心温度 (°C)
	UsagePct  float64 `json:"usage_pct"`  // 占用率 % (若驱动暴露 busy_percent，否则按性能档位估算)
	VramUsed  uint64  `json:"vram_used"`  // 专用显存/预留显存已用 (字节)
	VramTotal uint64  `json:"vram_total"` // 专用显存/预留显存总量 (字节)
	VramPct   float64 `json:"vram_pct"`   // 专用显存使用率 %
	GttUsed   uint64  `json:"gtt_used"`   // GTT 共享显存已用 (字节)
	GttTotal  uint64  `json:"gtt_total"`  // GTT 共享显存总量 (字节)
	GttPct    float64 `json:"gtt_pct"`    // GTT 共享显存使用率 %
	MemUsed   uint64  `json:"mem_used"`   // GPU 占用物理内存总计 (VramUsed + GttUsed, 字节)
	MemPct    float64 `json:"mem_pct"`    // GPU 占用物理内存占系统总内存的百分比 %
}

// readGPU 探测 Linux 系统下的显卡状态（支持 radeon、amdgpu、intel i915 等驱动）
func (c *Collector) readGPU(sysMemTotal uint64) *GPUStat {
	cards, err := filepath.Glob(c.path("sys/class/drm/card[0-9]*"))
	if err != nil || len(cards) == 0 {
		return nil
	}

	for _, card := range cards {
		devDir := filepath.Join(card, "device")
		if _, err := os.Stat(devDir); err != nil {
			continue
		}

		driverLink, _ := os.Readlink(filepath.Join(devDir, "driver"))
		driver := filepath.Base(driverLink)

		gpu := &GPUStat{
			Driver: driver,
			Model:  detectGPUModel(c.path("proc/bus/pci/devices"), devDir, driver),
		}

		// 1. 读取频率与温度 (通常由 hwmon 暴露)
		hwmonDirs, _ := filepath.Glob(filepath.Join(devDir, "hwmon/hwmon*"))
		if len(hwmonDirs) == 0 {
			hwmonDirs, _ = filepath.Glob(c.path("sys/class/hwmon/hwmon*"))
		}
		for _, hm := range hwmonDirs {
			nameB, _ := os.ReadFile(filepath.Join(hm, "name"))
			name := strings.TrimSpace(string(nameB))
			if name == driver || strings.Contains(name, "radeon") || strings.Contains(name, "amdgpu") {
				// freq1_input 单位为 Hz
				if fb, err := os.ReadFile(filepath.Join(hm, "freq1_input")); err == nil {
					var hz float64
					if _, err := fmt.Sscanf(strings.TrimSpace(string(fb)), "%g", &hz); err == nil && hz > 0 {
						gpu.FreqMHz = hz / 1e6
					}
				}
				// temp1_input 单位为毫摄氏度
				if tb, err := os.ReadFile(filepath.Join(hm, "temp1_input")); err == nil {
					var mc float64
					if _, err := fmt.Sscanf(strings.TrimSpace(string(tb)), "%g", &mc); err == nil {
						val := mc / 1000.0
						// AMD APU (Richland/Trinity) 内置传感器在低负载时往往输出负数或偏置值
						// 滤除 <= 0 的物理异常值，避免图表出现负数折线
						if val > 0 && val < 125 {
							gpu.TempC = val
						} else {
							gpu.TempC = 0 // 标记为不可用
						}
					}
				}
				break
			}
		}

		// 2. 占用率（amdgpu 会暴露 gpu_busy_percent，radeon 老驱动读 power_dpm 档位）
		if busyB, err := os.ReadFile(filepath.Join(devDir, "gpu_busy_percent")); err == nil {
			var pct float64
			if _, err := fmt.Sscanf(strings.TrimSpace(string(busyB)), "%g", &pct); err == nil {
				gpu.UsagePct = clampPct(pct)
			}
		} else {
			// 若无直接占用率，结合当前频率与基础频率计算相对负载率
			// A6-6420B 集显 Radeon HD 8470D 基础频率 304MHz, 加速频率 800MHz
			if gpu.FreqMHz > 0 {
				if gpu.FreqMHz <= 304 {
					gpu.UsagePct = 0.0 // 空闲低功耗档
				} else {
					gpu.UsagePct = clampPct(100.0 * (gpu.FreqMHz - 304.0) / (800.0 - 304.0))
				}
			}
		}

		// 3. 读取显存 (VRAM) 与共享内存 (GTT)
		c.readGPUMemory(card, devDir, driver, gpu, sysMemTotal)

		return gpu
	}

	return nil
}

// readGPUMemory 探测 GPU 显存 (VRAM) 与共享内存 (GTT)
func (c *Collector) readGPUMemory(cardDir, devDir, driver string, gpu *GPUStat, sysMemTotal uint64) {
	// 1. amdgpu sysfs 接口
	if vTotalB, err := os.ReadFile(filepath.Join(devDir, "mem_info_vram_total")); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(vTotalB)), "%d", &gpu.VramTotal)
	}
	if vUsedB, err := os.ReadFile(filepath.Join(devDir, "mem_info_vram_used")); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(vUsedB)), "%d", &gpu.VramUsed)
	}
	if gTotalB, err := os.ReadFile(filepath.Join(devDir, "mem_info_gtt_total")); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(gTotalB)), "%d", &gpu.GttTotal)
	}
	if gUsedB, err := os.ReadFile(filepath.Join(devDir, "mem_info_gtt_used")); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(gUsedB)), "%d", &gpu.GttUsed)
	}

	// 2. radeon 驱动 (从 debugfs/dri/X/radeon_vram_mm 与 radeon_gtt_mm 读取)
	if gpu.VramTotal == 0 && driver == "radeon" {
		cardName := filepath.Base(cardDir) // 如 "card0"
		cardIdx := strings.TrimPrefix(cardName, "card")
		if cardIdx == cardName {
			cardIdx = "0"
		}
		debugDriDir := c.path(filepath.Join("sys/kernel/debug/dri", cardIdx))

		// 解析 radeon_vram_mm
		if vramMM, err := os.ReadFile(filepath.Join(debugDriDir, "radeon_vram_mm")); err == nil {
			sizePages, usedBytes := parseRadeonMM(string(vramMM))
			if sizePages > 0 {
				gpu.VramTotal = sizePages * 4096
			}
			if usedBytes > 0 {
				gpu.VramUsed = usedBytes
			}
		}

		// 解析 radeon_gtt_mm
		if gttMM, err := os.ReadFile(filepath.Join(debugDriDir, "radeon_gtt_mm")); err == nil {
			sizePages, usedBytes := parseRadeonMM(string(gttMM))
			if sizePages > 0 {
				gpu.GttTotal = sizePages * 4096
			}
			if usedBytes > 0 {
				gpu.GttUsed = usedBytes
			}
		}
	}

	// 3. 兜底保护 (例如 debugfs 未赋权时，通过 PCI resource0 读取 VRAM BAR 大小，或保底估算)
	if gpu.VramTotal == 0 {
		if fi, err := os.Stat(filepath.Join(devDir, "resource0")); err == nil && fi.Size() > 0 {
			gpu.VramTotal = uint64(fi.Size())
		} else if strings.Contains(gpu.Model, "Radeon") {
			gpu.VramTotal = 256 * 1024 * 1024 // 默认 256MB
		}
	}
	if gpu.GttTotal == 0 && strings.Contains(gpu.Model, "Radeon") {
		gpu.GttTotal = 1024 * 1024 * 1024 // 默认 1024MB GTT 共享内存窗口
	}

	// 计算显存与共享内存占用率
	if gpu.VramTotal > 0 {
		gpu.VramPct = clampPct(100.0 * float64(gpu.VramUsed) / float64(gpu.VramTotal))
	}
	if gpu.GttTotal > 0 {
		gpu.GttPct = clampPct(100.0 * float64(gpu.GttUsed) / float64(gpu.GttTotal))
	}

	// GPU 实际占用的物理内存总计 (专用 VRAM 已用 + GTT 共享内存已用)
	gpu.MemUsed = gpu.VramUsed + gpu.GttUsed
	if sysMemTotal > 0 {
		gpu.MemPct = clampPct(100.0 * float64(gpu.MemUsed) / float64(sysMemTotal))
	}
}

// parseRadeonMM 解析 radeon_vram_mm / radeon_gtt_mm 头部
func parseRadeonMM(content string) (sizePages uint64, usedBytes uint64) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "size:") {
			fmt.Sscanf(trimmed, "size: %d", &sizePages)
		} else if strings.HasPrefix(trimmed, "usage:") {
			fmt.Sscanf(trimmed, "usage: %d", &usedBytes)
		}
		if sizePages > 0 && usedBytes > 0 {
			break
		}
	}
	return
}

func detectGPUModel(pciDevicesPath, devDir, driver string) string {
	// 尝试通过 uevent 获取 PCI 设备 ID
	ueventB, err := os.ReadFile(filepath.Join(devDir, "uevent"))
	if err == nil {
		content := string(ueventB)
		if strings.Contains(content, "PCI_ID=1002:9996") {
			return "Radeon HD 8470D (Richland)"
		}
	}
	if driver == "radeon" {
		return "Radeon HD Graphics"
	}
	return driver
}
