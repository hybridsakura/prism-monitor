package collect

import (
	"bufio"
	"io"
	"strings"
)

// mountEntry /proc/mounts 一行。
type mountEntry struct {
	dev, path, fs string
}

// allowedFs 只统计真实文件系统，过滤 overlay/tmpfs/proc 等虚拟挂载。
var allowedFs = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true,
	"xfs": true, "btrfs": true, "f2fs": true, "zfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true,
	"apfs": true, "hfsplus": true, "jfs": true, "reiserfs": true,
}

// parseMounts 解析 /proc/mounts，去重并按白名单过滤。
func parseMounts(r io.Reader) []mountEntry {
	seen := map[string]bool{}
	var out []mountEntry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		dev, path, fs := fields[0], unescapeMountPath(fields[1]), fields[2]
		if !allowedFs[fs] || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, mountEntry{dev: dev, path: path, fs: fs})
	}
	return out
}

// unescapeMountPath 处理 /proc/mounts 里的八进制转义（如 \040 空格）。
func unescapeMountPath(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

// statfsFunc statfs 注入点：Linux 下走 syscall，测试可替换。
// 返回 (总量, 可用量, 错误)，单位字节。
var statfsFunc = sysStatfs

// mountStat 对单个挂载点取容量。total<=0 时返回 false。
func mountStat(e mountEntry) (MountStat, bool) {
	total, avail, err := statfsFunc(e.path)
	if err != nil || total == 0 {
		return MountStat{}, false
	}
	used := uint64(0)
	if total > avail {
		used = total - avail
	}
	return MountStat{
		Path:  e.path,
		Dev:   e.dev,
		Fs:    e.fs,
		Total: total,
		Used:  used,
		Pct:   clampPct(100 * float64(used) / float64(total)),
	}, true
}
