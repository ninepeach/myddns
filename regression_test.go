package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigNormalization(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "test-token")
	for _, tc := range []struct {
		records string
		wantErr bool
	}{
		{`["HOME.Example.com.","*.example.com"]`, false},
		{`["HOME.Example.com.","home.example.com"]`, true},
		{`["home..example.com"]`, true},
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"zones":[{"id":"zone","records":`+tc.records+`}]}`), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if (err != nil) != tc.wantErr {
			t.Fatalf("records %s: %v", tc.records, err)
		}
		if !tc.wantErr && (cfg.Zones[0].Records[0] != "home.example.com" || cfg.CycleTimeout != "1m") {
			t.Fatalf("unexpected normalized config: %+v", cfg)
		}
	}
}

func TestCloudflareResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"result":null}` + strings.Repeat(" ", 1<<20)))
	}))
	defer server.Close()
	c := newCloudflareClient("test-token")
	c.baseURL = server.URL
	if _, err := c.request(context.Background(), http.MethodGet, "/", nil); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestReconcileSafetyAndPatch(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if r.URL.Query().Get("name.exact") != "home.example.com" || r.URL.Query().Get("type") != "A" {
						t.Error("missing exact name/type filters")
					}
					records := make([]DNSRecord, count)
					for i := range records {
						records[i] = DNSRecord{ID: "record", Name: "home.example.com", Type: "A", Content: "1.1.1.1"}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": records, "result_info": map[string]int{"total_pages": 1}})
					return
				}
				writes++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPatch || len(payload) != 1 || payload["content"] != "8.8.8.8" {
					t.Errorf("unexpected write: %s %+v", r.Method, payload)
				}
				_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
			}))
			defer server.Close()
			c := newCloudflareClient("test-token")
			c.baseURL = server.URL
			err := c.reconcile(context.Background(), "zone", "home.example.com", "A", netip.MustParseAddr("8.8.8.8"))
			if count == 1 && (err != nil || writes != 1) {
				t.Fatalf("expected one update: %v, %d", err, writes)
			}
			if count == 2 && (err == nil || writes != 0) {
				t.Fatalf("ambiguous records must not be updated: %v, %d", err, writes)
			}
			if err := c.reconcile(context.Background(), "zone", "home.example.com", "A", netip.MustParseAddr("::1")); err == nil {
				t.Fatal("invalid family/private IP accepted")
			}
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCycleTimeout(t *testing.T) {
	enabled, disabled := true, false
	app := &App{
		config: Config{CycleTimeout: "10ms", IPv4: &enabled, IPv6: &disabled},
		ipv4Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})},
	}
	if err := app.sync(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cycle deadline, got %v", err)
	}
}
