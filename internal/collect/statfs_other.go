//go:build !linux

package collect

import "fmt"

// sysStatfs 非 Linux 平台仅用于编译与测试，运行时不可用。
func sysStatfs(path string) (total, avail uint64, err error) {
	return 0, 0, fmt.Errorf("statfs 仅在 Linux 可用: %s", path)
}
