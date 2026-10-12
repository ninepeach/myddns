package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var ipServices = map[int][]string{
	4: {
		"https://api4.ipify.org",
		"https://ipv4.icanhazip.com",
	},
	6: {
		"https://api6.ipify.org",
		"https://ipv6.icanhazip.com",
	},
}

func newIPClient(family int) *http.Client {
	network := "tcp4"
	if family == 6 {
		network = "tcp6"
	}

	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}

	transport := &http.Transport{
		Proxy: nil,

		DialContext: func(
			ctx context.Context,
			_, address string,
		) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},

		TLSHandshakeTimeout: 5 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("HTTPS downgrade is forbidden")
			}
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func fetchIP(
	ctx context.Context,
	client *http.Client,
	endpoint string,
) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint,
		nil,
	)
	if err != nil {
		return netip.Addr{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf(
			"HTTP status %d",
			resp.StatusCode,
		)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1025))
	if err != nil {
		return netip.Addr{}, err
	}
	if len(body) > 1024 {
		return netip.Addr{}, errors.New("IP response too large")
	}

	ip, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid IP response: %w", err)
	}

	return ip, nil
}

var blockedIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

var blockedIPv6 = []netip.Prefix{
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func validPublicIP(ip netip.Addr, family int) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() {
		return false
	}

	if ip.Is4In6() {
		return false
	}

	switch family {
	case 4:
		if !ip.Is4() {
			return false
		}
		for _, prefix := range blockedIPv4 {
			if prefix.Contains(ip) {
				return false
			}
		}

	case 6:
		if !ip.Is6() {
			return false
		}
		for _, prefix := range blockedIPv6 {
			if prefix.Contains(ip) {
				return false
			}
		}

	default:
		return false
	}

	return true
}

func detectPublicIP(
	ctx context.Context,
	client *http.Client,
	family int,
) (netip.Addr, error) {
	services, ok := ipServices[family]
	if !ok {
		return netip.Addr{}, errors.New("unsupported IP family")
	}

	first, err := fetchIP(ctx, client, services[0])
	if err != nil {
		return netip.Addr{}, fmt.Errorf("first IP service: %w", err)
	}

	if !validPublicIP(first, family) {
		return netip.Addr{}, errors.New("first service returned non-public IP")
	}

	second, err := fetchIP(ctx, client, services[1])
	if err != nil {
		return netip.Addr{}, fmt.Errorf("second IP service: %w", err)
	}

	if !validPublicIP(second, family) {
		return netip.Addr{}, errors.New("second service returned non-public IP")
	}

	if first != second {
		return netip.Addr{}, fmt.Errorf(
			"IP mismatch: %s != %s",
			first,
			second,
		)
	}

	return first, nil
}
