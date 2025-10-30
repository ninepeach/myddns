package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/ninepeach/go-clog"
	"github.com/ninepeach/myddns/cloudflare"
	"github.com/ninepeach/myddns/utils"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const VERSION = "0.1.0"

var (
	token  = kingpin.Flag("token", "Cloudflare API Token").Required().String()
	zoneid = kingpin.Flag("zoneid", "Cloudflare Zone ID").Required().String()
	name   = kingpin.Flag("name", "Record Name (example.com)").Required().String()
	ifname = kingpin.Flag("ifname", "Local Interface Name (for source=iface)").Default("eth0").String()
	slack  = kingpin.Flag("slack", "Slack Webhook URL").Default("").String()
	source = kingpin.Flag("source", "IP source: public|iface").Default("public").String()
	period = kingpin.Flag("interval", "Update interval in seconds").Default("300").Int()
)

func init() {
	if err := log.NewConsole(); err != nil {
		panic("unable to create console logger: " + err.Error())
	}
}

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Version(VERSION)
	kingpin.Parse()

	if *slack != "" {
		if err := log.NewSlack(100, log.SlackConfig{Level: log.LevelInfo, URL: *slack}); err != nil {
			panic("unable to create slack logger: " + err.Error())
		}
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR2, syscall.SIGQUIT)

	loopTask(ch, time.Duration(*period))
	close(ch)
}

var lastIP string

func loopTask(sig chan os.Signal, interval time.Duration) {
	taskTicker := time.NewTicker(interval * time.Second)
	defer taskTicker.Stop()

	log.Info("Starting MyDDNS %s (interval=%ds, source=%s)", VERSION, *period, *source)
	doTask()

	for {
		select {
		case <-sig:
			log.Info("Received signal, exiting gracefully...")
			log.Stop()
			return
		case <-taskTicker.C:
			doTask()
		}
	}
}

func doTask() {
	var ipAddr string
	var err error

	ctx := context.Background()

	switch *source {
	case "public":
		ipAddr, err = utils.GetPublicIPv4(ctx)
	default:
		ipAddr, err = utils.GetIpv4AddrByInterfaceName(*ifname)
	}

	if err != nil {
		log.Error("Failed to get IP address: %v", err)
		return
	}

	if ipAddr == lastIP {
		log.Info("IP unchanged (%s), skip update", ipAddr)
		return
	}

	cc, err := cloudflare.NewCloudflareClient(*token, *zoneid, *name)
	if err != nil {
		log.Error("Failed to create Cloudflare client: %v", err)
		return
	}

	if err := cc.UpdateRecord(ipAddr); err != nil {
		log.Error("Failed to update DNS record: %v", err)
		return
	}

	lastIP = ipAddr
	log.Info("Updated DNS record successfully → %s", ipAddr)
}
