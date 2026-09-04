package patcher

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

var ErrNoAllowedIPs = errors.New("AllowedIPs line not found")

// Patch заменяет строки AllowedIPs в конфиге на переданный список CIDR.
func Patch(config string, cidrs []string) (string, error) {
	lines := strings.Split(config, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "AllowedIPs") {
			lines[i] = "AllowedIPs = " + strings.Join(cidrs, ", ")
			found = true
		}
	}
	if !found {
		return "", ErrNoAllowedIPs
	}
	return strings.Join(lines, "\n"), nil
}

var cgnat = mustCIDR("100.64.0.0/10")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func isBlocked(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip)
}

// ValidateAndClean проверяет список split-routing CIDR: только IPv4 без хост-битов,
// без приватных/служебных сетей и без 0.0.0.0/0. Возвращает отсортированный
// дедуплицированный список. Любая некорректная запись — ошибка: источник считается
// повреждённым, вызывающий код использует last-known-good.
func ValidateAndClean(cidrs []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		ip, n, err := net.ParseCIDR(c)
		if err != nil || n.IP.To4() == nil {
			return nil, fmt.Errorf("invalid ipv4 cidr %q", c)
		}
		if !ip.Equal(n.IP) {
			return nil, fmt.Errorf("cidr %q has host bits set", c)
		}
		if n.String() == "0.0.0.0/0" {
			return nil, errors.New("0.0.0.0/0 is not allowed in split-routing")
		}
		if isBlocked(n.IP) {
			return nil, fmt.Errorf("forbidden network %q", c)
		}
		key := n.String()
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty allowed-ips list")
	}
	sort.Strings(out)
	return out, nil
}
