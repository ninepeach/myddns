package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/ninepeach/go-clog"
	"github.com/ninepeach/myddns/cloudflare"
	"github.com/ninepeach/myddns/utils"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	token  = kingpin.Flag("token", "Api Token (string)").Required().String()
	zoneid = kingpin.Flag("zoneid", "Domain Zone ID (string)").Required().String()
	name   = kingpin.Flag("name", "Host Name (string)").Required().String()
	ifname = kingpin.Flag("ifname", "Interface Name (string)").Required().String()
	slack  = kingpin.Flag("slack", "Slack Webhook (string)").Default("").String()
)

var cc *cloudflare.Cloudflare

func init() {
	err := log.NewConsole()
	if err != nil {
		panic("unable to create new logger: " + err.Error())
	}
}

func main() {

	kingpin.HelpFlag.Short('h')
	kingpin.Version("0.0.1")
	kingpin.Parse()

	if len(*slack) > 0 {
		err := log.NewSlack(100,
			log.SlackConfig{
				Level: log.LevelInfo,
				URL:   *slack,
			},
		)
		if err != nil {
			panic("unable to create new slack logger: " + err.Error())
		}
	}

	cc, err := cloudflare.NewCloudflareClient(*token, *zoneid, *name)
	if err != nil {
		panic("unable to create CloudFlare client: " + err.Error())
	}
	_ = cc

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR2, syscall.SIGQUIT)

	//run loop
	loopTask(ch, 5)

	close(ch)
}

func loopTask(sig chan os.Signal, n time.Duration) {

	taskTicker := time.NewTicker(n * time.Second)
	defer taskTicker.Stop()

	doTask()
	for {
		select {
		case <-sig:
			//got signal and quit
			log.Info("got signal and quit")
			log.Stop()
			return
		case <-taskTicker.C:
			// do task
			doTask()
		}
	}
}

func doTask() {

	ipAddr, err := utils.GetIpv4AddrByInterfaceName(*ifname)

	if err != nil {
		log.Fatal("Error: %v", err)
		return
	}

	err = cc.UpdateRecord(ipAddr)
	if err != nil {
		log.Fatal("Error: %v", err)
	}
}
