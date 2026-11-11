//go:build !unix

package api

func diskSpace(string) (free, total uint64) {
	return 0, 0
}
