package orderstate

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func lockFile(f *os.File) error {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	var overlapped syscall.Overlapped
	ok, _, err := proc.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return fmt.Errorf("журнал уже занят или не поддерживает блокировку: %w", err)
	}
	return nil
}
