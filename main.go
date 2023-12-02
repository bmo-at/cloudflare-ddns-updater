package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
)

const (
	API_TOKEN_ENV_VARIABLE_NAME = "CLOUDFLARE_API_TOKEN"
	ZONE_ENV_VARIABLE_NAME      = "CLOUDFLARE_ZONE_NAME"
	RECORD_ENV_VARIABLE_NAME    = "CLOUDFLARE_RECORD_NAME"
	CURRNENT_IP_INFO_ENDPOINT   = "CURRENT_IP_INFO_ENDPOINT"
	DURATION_BETWEEN_UPDATES    = "DURATION_BETWEEN_UPDATES"
)

type CloudflareDDNSUpdaterApplication struct {
	api_token      string
	ip_info_url    string
	zone_name      string
	record_name    string
	sleep_interval time.Duration
	context        context.Context
	cancel         context.CancelFunc
	logger         *cloudflare.LeveledLogger
	api            *cloudflare.API
}

func (c *CloudflareDDNSUpdaterApplication) configure() {
	c.logger.Infof("CLOUDFLARE DDNS configuration started " + strings.Repeat("-", 12) + "\n")

	if api_token, exists := os.LookupEnv(API_TOKEN_ENV_VARIABLE_NAME); exists {
		c.api_token = api_token
	} else {
		c.logger.Errorf("no API token found in env var '%s'\n", API_TOKEN_ENV_VARIABLE_NAME)
		c.exit()
	}

	if zone_name, exists := os.LookupEnv(ZONE_ENV_VARIABLE_NAME); exists {
		c.zone_name = zone_name
	} else {
		c.logger.Errorf("no zone name found in env var '%s'\n", ZONE_ENV_VARIABLE_NAME)
		c.exit()
	}

	if record_name, exists := os.LookupEnv("CLOUDFLARE_RECORD_NAME"); exists {
		c.record_name = record_name
	} else {
		c.logger.Errorf("no record name found in env var '%s'\n", RECORD_ENV_VARIABLE_NAME)
		c.exit()
	}

	if custom_ip_info_url, exists := os.LookupEnv(CURRNENT_IP_INFO_ENDPOINT); exists {
		c.ip_info_url = custom_ip_info_url
	} else {
		c.ip_info_url = "https://ipinfo.io/ip"
	}

	if duration_string, exists := os.LookupEnv(DURATION_BETWEEN_UPDATES); exists {
		duration, err := time.ParseDuration(duration_string)
		if err != nil {
			c.logger.Errorf("custom duration between updates '%s' could not be parsed: '%s'\n", duration_string, err.Error())
			c.exit()
		}
		c.logger.Infof("custom duration betwwen updates was specified as '%s', using %s\n", duration_string, duration.String())
		c.sleep_interval = duration
	} else {
		c.sleep_interval = 5 * time.Minute
	}

	c.logger.Infof("CLOUDFLARE DDNS configuration finished " + strings.Repeat("-", 11) + "\n")
}

func (c *CloudflareDDNSUpdaterApplication) initialize() {
	c.logger.Infof("CLOUDFLARE DDNS initialization started " + strings.Repeat("-", 11) + "\n")

	api, err := cloudflare.NewWithAPIToken(c.api_token)
	if err != nil {
		c.logger.Errorf("could not create cloudflare api client with the provided token, %s\n", err.Error())
		c.exit()
	}
	c.api = api

	_, err = http.Get(c.ip_info_url)

	if err != nil {
		c.logger.Errorf("current ip info endpoint '%s' could not be requested: %s\n", c.ip_info_url, err.Error())
	}

	c.logger.Infof("CLOUDFLARE DDNS initialization finished " + strings.Repeat("-", 10) + "\n")
}

func (c *CloudflareDDNSUpdaterApplication) update(ctx context.Context) {
	c.logger.Infof("CLOUDFLARE DDNS update started " + strings.Repeat("-", 19) + "\n")

	ip_response, err := http.Get(c.ip_info_url)

	if err != nil {
		c.logger.Errorf("error when requesting the current ip from '%s': %s\n", c.ip_info_url, err.Error())
		c.exit()
	}

	ip_bytes, err := io.ReadAll(ip_response.Body)

	if err != nil {
		c.logger.Errorf("error reading the body of the ip request response: %s\n", err.Error())
		c.exit()
	}

	current_ip := net.ParseIP(string(ip_bytes))

	if current_ip == nil {
		c.logger.Errorf("current IP address could not be parsed from '%s'\n", string(ip_bytes))
		c.exit()
	}

	zones, err := c.api.ListZones(ctx, c.zone_name)

	if err != nil {
		c.logger.Errorf("could not list zones: %s\n", err.Error())
		c.exit()
	}

	if len(zones) < 1 {
		c.logger.Errorf("no zones found for '%s'\n", c.zone_name)
		c.exit()
	}

	rc := cloudflare.ZoneIdentifier(zones[len(zones)-1].ID)

	records, _, err := c.api.ListDNSRecords(ctx, rc, cloudflare.ListDNSRecordsParams{
		Type: "A",
		Name: c.record_name,
	})

	if err != nil {
		c.logger.Errorf("could not list records for '%s': %s\n", c.record_name, err.Error())
		c.exit()
	}

	if len(records) < 1 {
		c.logger.Errorf("no A records found for '%s'\n", c.record_name)
		c.exit()
	}

	if records[len(records)-1].Content != current_ip.String() {
		_, err := c.api.UpdateDNSRecord(ctx, rc, cloudflare.UpdateDNSRecordParams{
			ID:      records[len(records)-1].ID,
			Content: current_ip.String(),
		})

		if err != nil {
			c.logger.Errorf("could not update record '%s' in zone '%s': %s\n", c.record_name, c.zone_name, err)
			c.exit()
		}
	}
	c.logger.Infof("CLOUDFLARE DDNS update finished " + strings.Repeat("-", 18) + "\n")
}

func (c *CloudflareDDNSUpdaterApplication) run() {
	ctx, cancel := context.WithCancel(context.Background())
	c.context = ctx
	c.cancel = cancel
	for {
		go c.update(c.context)
		time.Sleep(c.sleep_interval)
	}
}

func (c *CloudflareDDNSUpdaterApplication) exit() {
	defer c.cancel()
	os.Exit(1)
}

func main() {
	app := new(CloudflareDDNSUpdaterApplication)
	app.logger = new(cloudflare.LeveledLogger)
	app.logger.Level = cloudflare.LevelInfo
	app.configure()
	app.initialize()
	app.run()
}
