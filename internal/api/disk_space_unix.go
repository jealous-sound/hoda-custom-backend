//go:build unix

package api

import "syscall"

func diskSpace(dir string) (free, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, 0
	}
	blockSize := uint64(stat.Bsize)
	return uint64(stat.Bavail) * blockSize, uint64(stat.Blocks) * blockSize
}
