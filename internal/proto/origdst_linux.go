//go:build linux

package proto

import (
	"net"
	"syscall"
	"unsafe"
)

const soOriginalDst = 80

// OriginalDst returns the pre-REDIRECT destination of a TCP connection that
// was hijacked by nftables/iptables REDIRECT. IPv4 only for now.
func OriginalDst(conn *net.TCPConn) (*net.TCPAddr, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var sa syscall.RawSockaddrInet4
	size := uint32(unsafe.Sizeof(sa))
	var sysErr error
	ctlErr := raw.Control(func(fd uintptr) {
		_, _, e := syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			syscall.IPPROTO_IP,
			soOriginalDst,
			uintptr(unsafe.Pointer(&sa)),
			uintptr(unsafe.Pointer(&size)),
			0,
		)
		if e != 0 {
			sysErr = e
		}
	})
	if ctlErr != nil {
		return nil, ctlErr
	}
	if sysErr != nil {
		return nil, sysErr
	}
	port := int(sa.Port>>8) | int(sa.Port&0xff)<<8
	return &net.TCPAddr{
		IP:   net.IPv4(sa.Addr[0], sa.Addr[1], sa.Addr[2], sa.Addr[3]),
		Port: port,
	}, nil
}
