package netalloc

import (
	"encoding/binary"
	"errors"
	"net"
)

var ErrNoFreeIPs = errors.New("no free IPs in subnet")

// Allocate возвращает первый свободный адрес подсети, пропуская адрес сети (.0),
// первый хост (.1, резерв VPN-сервера) и broadcast. Только IPv4.
func Allocate(cidr *net.IPNet, used []net.IP) (net.IP, error) {
	ip4 := cidr.IP.To4()
	if ip4 == nil {
		return nil, errors.New("only ipv4 subnets are supported")
	}
	n := binary.BigEndian.Uint32(ip4)
	mask := cidr.Mask
	if len(mask) == net.IPv6len {
		mask = mask[12:]
	}
	broadcast := n | ^binary.BigEndian.Uint32(mask)
	usedSet := map[uint32]bool{}
	for _, u := range used {
		if v := u.To4(); v != nil {
			usedSet[binary.BigEndian.Uint32(v)] = true
		}
	}
	for i := n + 2; i < broadcast; i++ {
		if !usedSet[i] {
			out := make(net.IP, 4)
			binary.BigEndian.PutUint32(out, i)
			return out, nil
		}
	}
	return nil, ErrNoFreeIPs
}
