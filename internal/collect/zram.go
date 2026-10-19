package collect

import (
	"fmt"
	"io"
	"strings"
)

// zramMMStat /sys/block/zram*/mm_stat 前三个字段。
type zramMMStat struct {
	origDataSize uint64 // 原始数据量
	comprDataSize uint64 // 压缩后数据量
	memUsedTotal uint64 // 实际占用内存
}

func parseMMStat(r io.Reader) (zramMMStat, error) {
	var s zramMMStat
	n, err := fmt.Fscanf(r, "%d %d %d", &s.origDataSize, &s.comprDataSize, &s.memUsedTotal)
	if err != nil && n < 3 {
		return s, fmt.Errorf("解析 mm_stat 失败: %w", err)
	}
	return s, nil
}

// parseCompAlgorithm 从 "lzo lz4 [zstd]" 中提取当前算法。
func parseCompAlgorithm(r io.Reader) string {
	b, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	for _, f := range strings.Fields(string(b)) {
		if strings.HasPrefix(f, "[") && strings.HasSuffix(f, "]") {
			return strings.TrimSuffix(strings.TrimPrefix(f, "["), "]")
		}
	}
	// 没有括号标注时退化为最后一个字段
	fields := strings.Fields(string(b))
	if len(fields) > 0 {
		return fields[len(fields)-1]
	}
	return ""
}

// zramStats 由 mm_stat + disksize + 算法装配 ZramStat。
func zramStats(dev string, mm zramMMStat, disksize uint64, algo string) ZramStat {
	ratio := 0.0
	if mm.comprDataSize > 0 {
		ratio = float64(mm.origDataSize) / float64(mm.comprDataSize)
	}
	return ZramStat{
		Dev:   dev,
		Algo:  algo,
		Used:  mm.memUsedTotal,
		Total: disksize,
		Orig:  mm.origDataSize,
		Ratio: ratio,
	}
}
