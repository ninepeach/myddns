package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

type CloudflareClient struct {
	client  *http.Client
	token   string
	baseURL string

	retryAfter time.Time
}

type DNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type cloudflareResponse struct {
	Success bool `json:"success"`

	Errors []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`

	Result json.RawMessage `json:"result"`

	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func newCloudflareClient(token string) *CloudflareClient {
	return &CloudflareClient{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		token:   token,
		baseURL: cloudflareAPI,
	}
}

func (c *CloudflareClient) request(
	ctx context.Context,
	method string,
	path string,
	payload any,
) (cloudflareResponse, error) {
	var result cloudflareResponse

	if time.Now().Before(c.retryAfter) {
		return result, fmt.Errorf(
			"Cloudflare rate limited until %s",
			c.retryAfter.Format(time.RFC3339),
		)
	}

	var body io.Reader

	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return result, err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+path,
		body,
	)
	if err != nil {
		return result, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		delay := 5 * time.Minute

		if value := resp.Header.Get("Retry-After"); value != "" {
			if seconds, err := strconv.Atoi(value); err == nil {
				if seconds > 0 {
					delay = time.Duration(seconds) * time.Second
				}
			} else if date, err := http.ParseTime(value); err == nil {
				if until := time.Until(date); until > 0 {
					delay = until
				}
			}
		}

		c.retryAfter = time.Now().Add(delay)
		return result, fmt.Errorf("Cloudflare rate limit: HTTP 429")
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return result, err
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf(
			"invalid Cloudflare response: %w",
			err,
		)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Success {
		messages := make([]string, 0, len(result.Errors))

		for _, item := range result.Errors {
			messages = append(
				messages,
				fmt.Sprintf("%d: %s", item.Code, item.Message),
			)
		}

		return result, fmt.Errorf(
			"Cloudflare HTTP %d: %s",
			resp.StatusCode,
			strings.Join(messages, "; "),
		)
	}

	return result, nil
}

func (c *CloudflareClient) listRecords(
	ctx context.Context,
	zoneID string,
	name string,
	recordType string,
) ([]DNSRecord, error) {
	var records []DNSRecord

	const perPage = 100

	for page := 1; ; page++ {
		params := url.Values{}
		params.Set("name.exact", name)
		params.Set("type", recordType)
		params.Set("page", strconv.Itoa(page))
		params.Set("per_page", strconv.Itoa(perPage))

		path := fmt.Sprintf(
			"/zones/%s/dns_records?%s",
			url.PathEscape(zoneID),
			params.Encode(),
		)

		response, err := c.request(
			ctx,
			http.MethodGet,
			path,
			nil,
		)
		if err != nil {
			return nil, err
		}

		var batch []DNSRecord
		if err := json.Unmarshal(response.Result, &batch); err != nil {
			return nil, err
		}

		for _, record := range batch {
			if strings.EqualFold(record.Name, name) &&
				record.Type == recordType {
				records = append(records, record)
			}
		}

		totalPages := response.ResultInfo.TotalPages
		if totalPages < 1 {
			return nil, errors.New("invalid Cloudflare pagination")
		}

		if page >= totalPages {
			break
		}
	}

	return records, nil
}

func (c *CloudflareClient) createRecord(
	ctx context.Context,
	zoneID string,
	name string,
	recordType string,
	ip netip.Addr,
) error {
	payload := struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		TTL     int    `json:"ttl"`
		Proxied bool   `json:"proxied"`
	}{
		Type:    recordType,
		Name:    name,
		Content: ip.String(),
		TTL:     1,
		Proxied: false,
	}

	path := fmt.Sprintf(
		"/zones/%s/dns_records",
		url.PathEscape(zoneID),
	)

	_, err := c.request(
		ctx,
		http.MethodPost,
		path,
		payload,
	)

	return err
}

func (c *CloudflareClient) updateRecord(
	ctx context.Context,
	zoneID string,
	recordID string,
	ip netip.Addr,
) error {
	payload := struct {
		Content string `json:"content"`
	}{
		Content: ip.String(),
	}

	path := fmt.Sprintf(
		"/zones/%s/dns_records/%s",
		url.PathEscape(zoneID),
		url.PathEscape(recordID),
	)

	_, err := c.request(
		ctx,
		http.MethodPatch,
		path,
		payload,
	)

	return err
}

func (c *CloudflareClient) reconcile(
	ctx context.Context,
	zoneID string,
	name string,
	recordType string,
	ip netip.Addr,
) error {
	if !ip.IsValid() {
		return errors.New("invalid desired IP")
	}

	records, err := c.listRecords(
		ctx,
		zoneID,
		name,
		recordType,
	)
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}

	switch len(records) {
	case 0:
		if err := c.createRecord(
			ctx,
			zoneID,
			name,
			recordType,
			ip,
		); err != nil {
			return fmt.Errorf("create record: %w", err)
		}

		logger.Info(
			"DNS record created",
			"name", name,
			"type", recordType,
			"ip", ip.String(),
		)

		return nil

	case 1:
		record := records[0]

		current, err := netip.ParseAddr(record.Content)
		if err == nil && current == ip {
			logger.Debug(
				"DNS unchanged",
				"name", name,
				"type", recordType,
			)
			return nil
		}

		if record.ID == "" {
			return errors.New("Cloudflare record ID is empty")
		}

		if err := c.updateRecord(
			ctx,
			zoneID,
			record.ID,
			ip,
		); err != nil {
			return fmt.Errorf("update record: %w", err)
		}

		logger.Info(
			"DNS record updated",
			"name", name,
			"type", recordType,
			"ip", ip.String(),
		)

		return nil

	default:
		return fmt.Errorf(
			"multiple %s records found for %s",
			recordType,
			name,
		)
	}
}
