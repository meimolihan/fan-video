package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ssrfBlockedIP 判断 IP 是否为私网/回环/链路本地/保留地址（SSRF 目标）。
func ssrfBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// IPv4 保留网段：0.0.0.0/8（"this network"）、100.64.0.0/10（CGNAT）、
	// 169.254.0.0/16（链路本地 IPv4，IsLinkLocalUnicast 覆盖）、
	// 192.0.0.0/24、198.18.0.0/15（基准测试）、240.0.0.0/4（组播/保留）。
	if v4 := ip.To4(); v4 != nil {
		first := v4[0]
		if first == 0 || first == 100 || first == 198 || first >= 240 {
			return true
		}
		if first == 192 {
			second := v4[1]
			if second == 0 && v4[2] == 0 && v4[3] >= 0 { // 192.0.0.0/24
				return true
			}
			if second == 168 { // 192.168.0.0/16 (IsPrivate 已覆盖，冗余防御)
				return true
			}
		}
		if first == 172 && v4[1] >= 16 && v4[1] <= 31 { // 172.16.0.0/12 (IsPrivate 已覆盖，冗余防御)
			return true
		}
		if first == 10 { // 10.0.0.0/8 (IsPrivate 已覆盖，冗余防御)
			return true
		}
	}
	return false
}

// ssrfSafeDialer 返回拒绝私网目标的 DialContext。
func ssrfSafeDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if ip := net.ParseIP(host); ip != nil {
			if ssrfBlockedIP(ip) {
				return nil, fmt.Errorf("目标地址被安全策略拒绝（禁止访问私网/保留地址）")
			}
		} else {
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("解析目标地址失败: %w", err)
			}
			for _, ia := range ips {
				if ssrfBlockedIP(ia.IP) {
					return nil, fmt.Errorf("目标地址解析到受限网段，已拦截")
				}
			}
		}
		return d.DialContext(ctx, network, addr)
	}
}

// newSSRFSafeClient 创建带 SSRF 防护与超时的 HTTP 客户端。
func newSSRFSafeClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext:         ssrfSafeDialer(),
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  false,
		MaxIdleConnsPerHost: 2,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// validateDownloadURL 校验下载 URL 的 scheme 与 host。
func validateDownloadURL(imageURL string) error {
	u, err := url.Parse(imageURL)
	if err != nil {
		return fmt.Errorf("非法 URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("仅支持 http/https 协议的图片 URL")
	}
	if u.Host == "" {
		return fmt.Errorf("URL 缺少主机名")
	}
	return nil
}
