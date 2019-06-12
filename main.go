package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"

	log "github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v1"
)

var Version string

func main() {
	app := cli.NewApp()
	app.Name = "Kamailio exporter"
	app.Usage = "Expose Kamailio statistics as http endpoint for prometheus."
	app.Version = Version
	// define cli flags
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:   "debug",
			Usage:  "Enable debug logging",
			EnvVar: "DEBUG",
		},
		cli.StringFlag{
			Name:   "socketPath",
			Value:  "/var/run/kamailio/kamailio_ctl",
			Usage:  "Path to Kamailio unix domain socket",
			EnvVar: "SOCKET_PATH",
		},
		cli.StringFlag{
			Name:   "host",
			Usage:  "Kamailio ip or hostname. Domain socket is used if no host is defined.",
			EnvVar: "HOST",
		},
		cli.IntFlag{
			Name:   "port",
			Value:  3012,
			Usage:  "Kamailio port",
			EnvVar: "PORT",
		},
		cli.StringFlag{
			Name:   "bindIp",
			Value:  "0.0.0.0",
			Usage:  "Listen on this ip for scrape requests",
			EnvVar: "BIND_IP",
		},
		cli.IntFlag{
			Name:   "bindPort",
			Value:  9494,
			Usage:  "Listen on this port for scrape requests",
			EnvVar: "BIND_PORT",
		},
		cli.StringFlag{
			Name:   "metricsPath",
			Value:  "/metrics",
			Usage:  "The http scrape path",
			EnvVar: "METRICS_PATH",
		},
		cli.StringFlag{
			Name:   "gatewayHost",
			Value:  "127.0.0.1",
			Usage:  "Prometheus gateway ip to push metrics to",
			EnvVar: "GATEWAY_HOST",
		},
		cli.IntFlag{
			Name:   "gatewayPort",
			Value:  9091,
			Usage:  "Prometheus gateway port to push metrics to",
			EnvVar: "GATEWAY_PORT",
		},
		cli.IntFlag{
			Name:   "pushInterval",
			Value:  0,
			Usage:  "Interval to push metrics ( seconds )",
			EnvVar: "PUSH_INTERVAL",
		},
	}
	app.Action = appAction
	// then start the application
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// start the application
func appAction(c *cli.Context) error {
	log.Info("Starting kamailio exporter")

	if c.Bool("debug") {
		log.SetLevel(log.DebugLevel)
		log.Debug("Debug logging is enabled")
	}

	// create a collector
	collector, err := NewStatsCollector(c)
	if err != nil {
		return err
	}
	// and register it in prometheus API
	prometheus.MustRegister(collector)

	// start gorouting to push metrics
	if pushInterval := c.Int("pushInterval"); pushInterval > 0 {
		go func() {
			pushAddress := fmt.Sprintf("%s:%d", c.String("gatewayHost"), c.Int("gatewayPort"))
			client := push.New(pushAddress, "kamailio")
			client.Collector(collector)

			host := c.String("host")
			if host == "" {
				var err error
				host, err = os.Hostname()

				if err != nil {
					log.WithError(err).Error("Unable to get hostname.")
				}
			}
			client.Grouping("instance", host)

			for {
				err := client.Add()
				if err != nil {
					log.Errorf("Unable to push metrics to %s: %s", pushAddress, err)
					os.Exit(1)
				}
				log.Info("Pushed metrics.")

				time.Sleep(time.Duration(pushInterval) * time.Second)
			}
		}()
	}

	metricsPath := c.String("metricsPath")
	listenAddress := fmt.Sprintf("%s:%d", c.String("bindIp"), c.Int("bindPort"))
	// wire "/" to return some helpful info
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Kamailio Exporter</title></head>
             <body>
			 <p>This is a prometheus metric exporter for Kamailio.</p>
			 <p>Browse <a href='` + metricsPath + `'>` + metricsPath + `</a> 
			 to get the metrics.</p>
             </body>
             </html>`))
	})
	// wire "/metrics" -> prometheus API collectors
	http.HandleFunc(metricsPath, promhttp.Handler().ServeHTTP)

	// start http server
	log.Info("Listening on ", listenAddress, metricsPath)
	return http.ListenAndServe(listenAddress, nil)
}
