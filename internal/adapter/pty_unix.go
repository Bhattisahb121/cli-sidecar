//go:build !windows

package adapter

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// startPTY starts the command in a new pseudo-terminal and returns the master fd.
func startPTY(cmd *exec.Cmd) (*os.File, error) {
	ptmx, pts, err := openPTY()
	if err != nil {
		return nil, err
	}

	cmd.Stdin = pts
	cmd.Stdout = pts
	cmd.Stderr = pts
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
	}

	if err := cmd.Start(); err != nil {
		ptmx.Close()
		pts.Close()
		return nil, err
	}

	pts.Close()
	return ptmx, nil
}

func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}

	// grantpt
	if err := grantpt(master); err != nil {
		master.Close()
		return nil, nil, err
	}

	// unlockpt
	if err := unlockpt(master); err != nil {
		master.Close()
		return nil, nil, err
	}

	// ptsname
	name, err := ptsname(master)
	if err != nil {
		master.Close()
		return nil, nil, err
	}

	slave, err = os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}

	return master, slave, nil
}

func ptsname(f *os.File) (string, error) {
	var n uint32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
	if errno != 0 {
		return "", errno
	}
	return "/dev/pts/" + itoa(int(n)), nil
}

func grantpt(f *os.File) error {
	// On Linux, grantpt is a no-op when devpts is mounted properly
	return nil
}

func unlockpt(f *os.File) error {
	var unlock int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		return errno
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
