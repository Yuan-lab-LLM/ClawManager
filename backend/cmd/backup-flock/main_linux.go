//go:build linux

// backup-flock supplies only the inherited-descriptor operation used by the
// MySQL backup script. The stock MySQL image does not include util-linux flock.
package main

import (
	"os"
	"strconv"
	"syscall"
)

func run(args []string) int {
	if len(args) != 3 || args[0] != "-n" || args[1] != "-x" {
		return 2
	}
	fd, err := strconv.Atoi(args[2])
	if err != nil || fd < 3 || fd > 1024 {
		return 2
	}
	var stat syscall.Stat_t
	if syscall.Fstat(fd, &stat) != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return 2
	}
	// flock belongs to the open file description inherited from the shell.
	// Exiting this child retains ownership until the shell closes its descriptor.
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return 1
	}
	return 0
}
func main() { os.Exit(run(os.Args[1:])) }
