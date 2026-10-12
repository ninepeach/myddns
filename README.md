# myddns

A minimal Cloudflare Dynamic DNS client written in Go.

myddns periodically detects public IPv4 and IPv6 addresses
using two independent HTTPS services and synchronizes
Cloudflare DNS A/AAAA records.

## Features

- IPv4 and IPv6 support
- Dual-source public IP verification
- Cloudflare DNS integration
- Multiple zones and hostnames
- Automatic DNS record creation
- Update only when IP changes
- No persistent state
- No third-party dependencies
- Single static binary

## Requirements

- Go 1.25+
- Cloudflare API Token with DNS edit permissions

## Configuration

Create `config.json`:

```json
{
  "interval": "5m",
  "cycle_timeout": "1m",
  "ipv4": true,
  "ipv6": true,
  "zones": [
    {
      "id": "YOUR_CLOUDFLARE_ZONE_ID",
      "records": [
        "home.example.com"
      ]
    }
  ]
}
```

Set the Cloudflare API Token:

```bash
export CLOUDFLARE_API_TOKEN="YOUR_TOKEN"
```

## Build

```bash
make build
```

## Run

Run once:

```bash
./myddns -config config.json -once
```

Run continuously:

```bash
./myddns -config config.json
```

The default update interval is 5 minutes.

Each sync cycle (including one-shot runs) has a total timeout of 1 minute,
configurable with `cycle_timeout`. Increase it for large record sets.
Record names are normalized to lowercase with one trailing dot removed.
Wildcard names such as `*.example.com` are supported; internationalized
names must use ASCII Punycode. Duplicate names are rejected after normalization.

## Linux Service

myddns can run as a systemd or OpenRC service.

It runs in the foreground and handles SIGTERM gracefully.

## Security

- Uses HTTPS for IP detection and Cloudflare API requests.
- Requires two independent IP services to return the same address.
- Validates public IP addresses.
- Preserves existing DNS record properties.
- Never deletes DNS records.
- Does not store API tokens in configuration files.

## License

See LICENSE if provided.
