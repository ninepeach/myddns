package cloudflare

import (
	"errors"
	"fmt"
	"log"
)

type Cloudflare struct {
	client *CloudflareAPI
}

func NewCloudflareClient(token string, zoneid string, host string) (*Cloudflare, error) {

	api, err := NewCloudflareAPI(token, zoneid, host)

	if err != nil {
		return nil, err
	}

	c := &Cloudflare{
		client: api,
	}

	return c, nil
}

func (c *Cloudflare) UpdateRecord(ip string) error {
	records, err := c.client.ListDNSRecords(RecordTypeA)
	if err != nil {
		return err
	}

	var record Record
	for i := range records {
		fmt.Println(records[i].Name, c.client.Host)
		if records[i].Name == c.client.Host {
			record = records[i]
		}
	}

	if record == (Record{}) {
		return errors.New("Host not found")
	}

	if ip != record.Content {
		record.Content = ip
		err = c.client.UpdateDNSRecord(record)
		if err != nil {
			return err
		}
		log.Printf("IP changed, updated to %s", ip)
	} else {
		log.Print("No change in IP, not updating record")
	}

	return nil
}

func (c *Cloudflare) UpdateRecord6(ip string) error {
	records, err := c.client.ListDNSRecords(RecordTypeAAAA)
	if err != nil {
		return err
	}

	var record Record
	for i := range records {
		if records[i].Name == c.client.Host {
			record = records[i]
		}
	}

	if record == (Record{}) {
		return errors.New("Host not found")
	}

	if ip != record.Content {
		record.Content = ip
		err = c.client.UpdateDNSRecord(record)
		if err != nil {
			return err
		}
		log.Printf("IP changed, updated to %s", ip)
	} else {
		log.Print("No change in IP, not updating record")
	}

	return nil
}
