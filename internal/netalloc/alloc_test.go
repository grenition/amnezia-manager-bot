package netalloc

import (
	"errors"
	"net"
	"strconv"
	"testing"
)

func ip(s string) net.IP { return net.ParseIP(s) }

func netw(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestAllocate(t *testing.T) {
	got, err := Allocate(netw("10.8.1.0/24"), nil)
	if err != nil || got.String() != "10.8.1.2" {
		t.Fatalf("got %v %v", got, err)
	}
	got, err = Allocate(netw("10.8.1.0/24"), []net.IP{ip("10.8.1.2"), ip("10.8.1.3")})
	if err != nil || got.String() != "10.8.1.4" {
		t.Fatalf("got %v %v", got, err)
	}
	// .0 (сеть) и .1 (резерв VPN-сервера) никогда не выдаются
	got, err = Allocate(netw("10.8.1.0/24"), []net.IP{ip("10.8.1.0"), ip("10.8.1.1")})
	if err != nil || got.String() != "10.8.1.2" {
		t.Fatalf("got %v %v", got, err)
	}
	// несмежные занятые
	got, err = Allocate(netw("10.9.0.0/24"), []net.IP{ip("10.9.0.2"), ip("10.9.0.5")})
	if err != nil || got.String() != "10.9.0.3" {
		t.Fatalf("got %v %v", got, err)
	}
	// полный диапазон: последний валидный .254, broadcast .255 недоступен
	var used []net.IP
	for i := 2; i <= 254; i++ {
		used = append(used, ip("10.8.1."+strconv.Itoa(i)))
	}
	if _, err := Allocate(netw("10.8.1.0/24"), used); !errors.Is(err, ErrNoFreeIPs) {
		t.Fatalf("want ErrNoFreeIPs, got %v", err)
	}
	got, err = Allocate(netw("10.8.1.0/24"), used[:len(used)-1])
	if err != nil || got.String() != "10.8.1.254" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestAllocateIPv6Rejected(t *testing.T) {
	if _, err := Allocate(netw("fd00::/64"), nil); err == nil {
		t.Fatal("ipv6 must be rejected")
	}
}
