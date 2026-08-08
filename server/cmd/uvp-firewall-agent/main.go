package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"uvplatform.cn/uvp-gb28181/app/gb28181/security/firewall"
)

func main() {
	socket := flag.String("socket", "/run/uvp/firewall-agent.sock", "Unix socket path")
	flag.Parse()
	stop := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-signals; close(stop) }()
	if err := firewall.New(nil, nil, nil).ServeUnix(*socket, stop); err != nil {
		os.Exit(1)
	}
}
