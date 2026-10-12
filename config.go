package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Config struct {
	CycleTimeout string `json:"cycle_timeout"`
	Interval     string `json:"interval"`
	IPv4         *bool  `json:"ipv4"`
	IPv6         *bool  `json:"ipv6"`
	Zones        []Zone `json:"zones"`
}

type Zone struct {
	ID      string   `json:"id"`
	Records []string `json:"records"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config

	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode config: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return cfg, errors.New("unexpected trailing JSON data")
	}

	if cfg.Interval == "" {
		cfg.Interval = "5m"
	}
	if cfg.CycleTimeout == "" {
		cfg.CycleTimeout = "1m"
	}
	for i := range cfg.Zones {
		for j, name := range cfg.Zones[i].Records {
			cfg.Zones[i].Records[j] = strings.ToLower(strings.TrimSuffix(name, "."))
		}
	}

	if cfg.IPv4 == nil {
		v := true
		cfg.IPv4 = &v
	}

	if cfg.IPv6 == nil {
		v := true
		cfg.IPv6 = &v
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.CycleTimeout != "" {
		if timeout, err := time.ParseDuration(c.CycleTimeout); err != nil || timeout <= 0 {
			return errors.New("cycle_timeout must be a positive duration")
		}
	}
	interval, err := time.ParseDuration(c.Interval)
	if err != nil || interval < time.Minute {
		return errors.New("interval must be at least 1m")
	}

	if c.IPv4 == nil || c.IPv6 == nil {
		return errors.New("IP family defaults must be initialized")
	}
	if !*c.IPv4 && !*c.IPv6 {
		return errors.New("IPv4 and IPv6 cannot both be disabled")
	}

	if len(c.Zones) == 0 {
		return errors.New("at least one zone is required")
	}

	zoneIDs := make(map[string]bool)

	for _, zone := range c.Zones {
		if strings.TrimSpace(zone.ID) == "" {
			return errors.New("zone ID cannot be empty")
		}

		if zoneIDs[zone.ID] {
			return fmt.Errorf("duplicate zone: %s", zone.ID)
		}
		zoneIDs[zone.ID] = true

		if len(zone.Records) == 0 {
			return fmt.Errorf("zone %s has no records", zone.ID)
		}

		records := make(map[string]bool)

		for _, name := range zone.Records {
			if err := validateDNSName(name); err != nil {
				return fmt.Errorf("invalid record %q: %w", name, err)
			}

			key := strings.ToLower(name)
			if records[key] {
				return fmt.Errorf("duplicate record: %s", name)
			}
			records[key] = true
		}
	}

	if os.Getenv("CLOUDFLARE_API_TOKEN") == "" {
		return errors.New("CLOUDFLARE_API_TOKEN is required")
	}

	return nil
}

func validateDNSName(name string) error {
	if len(name) == 0 || len(name) > 253 {
		return errors.New("invalid DNS name length")
	}

	if name != strings.ToLower(name) {
		return errors.New("DNS name must be lowercase")
	}

	if strings.HasSuffix(name, ".") {
		return errors.New("trailing dot is not allowed")
	}

	for i, label := range strings.Split(name, ".") {
		if i == 0 && label == "*" && strings.Contains(name, ".") {
			continue
		}
		if len(label) == 0 || len(label) > 63 {
			return errors.New("invalid DNS label length")
		}

		if label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("DNS label cannot begin or end with hyphen")
		}

		for _, ch := range label {
			if (ch < 'a' || ch > 'z') &&
				(ch < '0' || ch > '9') &&
				ch != '-' {
				return errors.New("invalid DNS character")
			}
		}
	}

	return nil
}
