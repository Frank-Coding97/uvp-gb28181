package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/security/firewall"
)

func main() {
	socket := flag.String("socket", "/run/uvp/firewall-agent.sock", "Unix socket path")
	port := flag.Uint("sip-port", 56002, "published SIP port")
	allowlistText := flag.String("allowlist", "127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16", "comma-separated source networks never blocked")
	flag.Parse()
	allowlist, err := parseAllowlists(*allowlistText)
	if err != nil {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	config, err := firewall.DetectNftConfig(ctx, nil, uint16(*port), allowlist)
	if err == nil {
		var backend firewall.Backend
		backend, err = firewall.NewNftBackend(ctx, nil, config)
		if err == nil {
			defer cancel()
			stop := make(chan struct{})
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
			go func() { <-signals; close(stop) }()
			if err := firewall.New(backend, nil, allowlist).ServeUnix(*socket, stop); err != nil {
				os.Exit(1)
			}
			return
		}
	}
	cancel()
	os.Exit(1)
}

func parseAllowlists(raw string) ([]net.IPNet, error) {
	items := make([]net.IPNet, 0)
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		items = append(items, *network)
	}
	return items, nil
}
