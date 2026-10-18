package server

import (
	"net"
	"strings"
	"syscall"
	"unsafe"
)

// addrDump asks the kernel for its IPv4 address table over netlink, like
// syscall.NetlinkRIB but without bind(2), which Android refuses apps (it is
// how bionic's getifaddrs does it too).
func addrDump() ([]byte, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	// Two seconds at most: a kernel that doesn't answer shouldn't hang `gecko share`.
	tv := syscall.Timeval{Sec: 2}
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	req := make([]byte, syscall.NLMSG_HDRLEN+syscall.SizeofRtGenmsg)
	*(*uint32)(unsafe.Pointer(&req[0])) = uint32(len(req))
	*(*uint16)(unsafe.Pointer(&req[4])) = syscall.RTM_GETADDR
	*(*uint16)(unsafe.Pointer(&req[6])) = syscall.NLM_F_DUMP | syscall.NLM_F_REQUEST
	*(*uint32)(unsafe.Pointer(&req[8])) = 1 // sequence number
	req[syscall.NLMSG_HDRLEN] = syscall.AF_INET
	if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, err
	}
	var tab []byte
	buf := make([]byte, 1<<16)
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			return nil, err
		}
		msgs, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return nil, err
		}
		tab = append(tab, buf[:n]...)
		for _, m := range msgs {
			if m.Header.Type == syscall.NLMSG_DONE || m.Header.Type == syscall.NLMSG_ERROR {
				return tab, nil
			}
		}
	}
}

// labeledAddrs lists IPv4 addresses with their interface labels straight
// from the kernel's address table (RTM_GETADDR), for when the interface
// table can't be read: Android doesn't let apps targeting API 30 or newer
// ask for links (RTM_GETLINK), which net.Interfaces needs.
func labeledAddrs() []ifaceAddr {
	tab, err := addrDump()
	if err != nil {
		return nil
	}
	msgs, err := syscall.ParseNetlinkMessage(tab)
	if err != nil {
		return nil
	}
	var out []ifaceAddr
	for _, m := range msgs {
		if m.Header.Type != syscall.RTM_NEWADDR {
			continue
		}
		attrs, err := syscall.ParseNetlinkRouteAttr(&m)
		if err != nil {
			continue
		}
		var a ifaceAddr
		for _, at := range attrs {
			switch at.Attr.Type {
			case syscall.IFA_LABEL:
				a.Name = strings.TrimRight(string(at.Value), "\x00")
			case syscall.IFA_LOCAL:
				if len(at.Value) == 4 {
					a.IP = net.IP(append([]byte(nil), at.Value...))
				}
			case syscall.IFA_ADDRESS:
				if a.IP == nil && len(at.Value) == 4 {
					a.IP = net.IP(append([]byte(nil), at.Value...))
				}
			}
		}
		if a.IP != nil && !a.IP.IsLoopback() {
			out = append(out, a)
		}
	}
	return out
}
