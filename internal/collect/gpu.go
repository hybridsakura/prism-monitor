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
	VramUsed  uint64  `json:"vram_used"`  // 显存/共享显存已用 (字节)
	VramTotal uint64  `json:"vram_total"` // 显存总量 (字节)
}

// readGPU 探测 Linux 系统下的显卡状态（支持 radeon、amdgpu、intel i915 等驱动）
func (c *Collector) readGPU() *GPUStat {
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
						gpu.TempC = mc / 1000.0
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

		return gpu
	}

	return nil
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
