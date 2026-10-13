//go:build unix

package cache

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive flock on f without waiting: false if someone
// else has it. flock locks belong to the open file, not the process, so two
// opens of one file in the same process exclude each other too.
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(f *os.File) { syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
