//go:build !linux

package proto

import (
	"errors"
	"net"
)

// OriginalDst is a stub for non-Linux builds (used for local development on
// macOS). The real implementation requires SO_ORIGINAL_DST which is Linux-only.
func OriginalDst(_ *net.TCPConn) (*net.TCPAddr, error) {
	return nil, errors.New("SO_ORIGINAL_DST only supported on Linux")
}
