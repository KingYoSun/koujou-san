//go:build !windows

package chatgptplan

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func protect(file string) error { return os.Chmod(file, 0600) }
func privateRead(file string) error {
	i, err := os.Stat(file)
	if err != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
		return ErrAuthentication
	}
	return nil
}
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
