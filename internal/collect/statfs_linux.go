//go:build linux

package collect

import "syscall"

// sysStatfs Linux 下通过 statfs(2) 取挂载点容量。
// 可用量按 Bavail（普通用户可用）计，与 df 行为一致。
func sysStatfs(path string) (total, avail uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return st.Blocks * bsize, st.Bavail * bsize, nil
}
