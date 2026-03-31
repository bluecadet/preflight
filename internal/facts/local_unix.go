//go:build !windows

package facts

import (
	"os"
	"syscall"
)

func gatherLocalDisks() ([]DiskFacts, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return nil, err
	}
	const gb = 1 << 30
	total := float64(stat.Blocks) * float64(stat.Bsize)
	free := float64(stat.Bfree) * float64(stat.Bsize)
	return []DiskFacts{{
		Path:    "/",
		TotalGB: total / gb,
		FreeGB:  free / gb,
		UsedGB:  (total - free) / gb,
	}}, nil
}

func gatherLocalEnv() map[string]string {
	env := make(map[string]string)
	for _, e := range os.Environ() {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				env[e[:i]] = e[i+1:]
				break
			}
		}
	}
	return env
}
