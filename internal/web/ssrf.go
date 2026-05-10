package web

import (
	"net"
	"net/url"
)

// IsPrivateHost 检查主机是否为私有/内网地址（SSRF 防护）
func IsPrivateHost(host string) bool {
	// 去除端口
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host // 没有端口，直接使用
	}
	// 域名检测：常见私有域名
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	// 解析 IP
	ip := net.ParseIP(h)
	if ip == nil {
		// 尝试 DNS 解析
		ips, err := net.LookupIP(h)
		if err != nil {
			return false // 无法解析，放行
		}
		ip = ips[0]
	}
	return isPrivateIP(ip)
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// 检查 IPv4 私有范围
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 127.0.0.0/8
		if ip4[0] == 127 {
			return true
		}
		// 169.254.0.0/16 (link-local)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		return false
	}
	// IPv6
	// ::1 (localhost)
	if ip.Equal(net.IPv6loopback) {
		return true
	}
	// fc00::/7 (unique local)
	if len(ip) == 16 && ip[0]&0xfe == 0xfc {
		return true
	}
	return false
}

// IsPrivateURL 检查 URL 是否指向私有地址
func IsPrivateURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return IsPrivateHost(u.Host)
}
