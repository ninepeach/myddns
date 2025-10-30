package utils

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

var publicIPProviders = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://ipinfo.io/ip",
}

func GetPublicIPv4(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for _, url := range publicIPProviders {
		req, _ := http.NewRequestWithContext(reqCtx, "GET", url, nil)
		req.Header.Set("User-Agent", "myddns/0.1.0")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if isValidIPv4(ip) {
			return ip, nil
		}
	}
	return "", errors.New("no public IPv4 provider succeeded")
}

func isValidIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil
}
