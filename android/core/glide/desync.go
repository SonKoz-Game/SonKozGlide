//go:build linux

package glide

import (
	"errors"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

const defaultHopLimit = 64

func writeDesynced(remote net.Conn, first []byte, s strategy) error {
	pos := splitPoint(first, s)
	if pos <= 0 || pos >= len(first) {
		pos = 1
		if len(first) < 2 {
			_, err := remote.Write(first)
			return err
		}
	}
	switch s.Mode {
	case modeSplit:
		return writeSplit(remote, first, pos)
	case modeFakeTTL:
		return writeFake(remote, first, pos, s)
	default:
		_, err := remote.Write(first)
		return err
	}
}

func writeSplit(remote net.Conn, first []byte, pos int) error {
	if _, err := remote.Write(first[:pos]); err != nil {
		return err
	}
	_, err := remote.Write(first[pos:])
	return err
}

func writeFake(remote net.Conn, first []byte, pos int, s strategy) error {
	tcp, ok := remote.(*net.TCPConn)
	if !ok {
		return errors.New("tcp degil")
	}
	raw, err := tcp.SyscallConn()
	if err != nil {
		return err
	}
	isV6 := remoteIsV6(tcp)
	fake := make([]byte, pos)
	copy(fake, first[:pos])

	var ctrlErr error
	err = raw.Control(func(fd uintptr) {
		sfd := int(fd)
		if setHopLimit(sfd, isV6, s.TTL) != nil {
			ctrlErr = errors.New("ttl ayarlanamadi")
			return
		}
		if spliceGift(sfd, fake) != nil {
			ctrlErr = errors.New("splice basarisiz")
		}
		copy(fake, first[:pos])
		setHopLimit(sfd, isV6, defaultHopLimit)
	})
	if err != nil {
		return err
	}
	if ctrlErr != nil {
		return ctrlErr
	}
	_, werr := tcp.Write(first)
	return werr
}

func spliceGift(sock int, data []byte) error {
	fds := make([]int, 2)
	if err := unix.Pipe2(fds, unix.O_CLOEXEC); err != nil {
		return err
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])

	var iov unix.Iovec
	iov.Base = &data[0]
	iov.SetLen(len(data))
	n, _, errno := unix.Syscall6(unix.SYS_VMSPLICE, uintptr(fds[1]), uintptr(unsafe.Pointer(&iov)), 1, unix.SPLICE_F_GIFT, 0, 0)
	if errno != 0 {
		return errno
	}
	remaining := int(n)
	for remaining > 0 {
		moved, err := unix.Splice(fds[0], nil, sock, nil, remaining, unix.SPLICE_F_MOVE)
		if err != nil {
			return err
		}
		if moved <= 0 {
			break
		}
		remaining -= int(moved)
	}
	return nil
}

func setHopLimit(fd int, isV6 bool, ttl int) error {
	if ttl <= 0 {
		ttl = defaultHopLimit
	}
	if isV6 {
		return unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, ttl)
	}
	return unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TTL, ttl)
}

func remoteIsV6(tcp *net.TCPConn) bool {
	ra, ok := tcp.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return false
	}
	return ra.IP.To4() == nil
}
