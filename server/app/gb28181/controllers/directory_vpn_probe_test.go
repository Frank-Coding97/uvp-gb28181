package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

// TestDirectoryVPNLatency is an explicitly enabled, read-only network baseline.
// It uses the existing project target; only SELECT 1 is issued, never fixture writes.
func TestDirectoryVPNLatency(t *testing.T) {
	if os.Getenv("UVP_DIRECTORY_VPN_PROBE") != "1" {
		t.Skip("explicit read-only VPN probe required")
	}
	path := os.Getenv("UVP_DIRECTORY_VPN_CONFIG")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read specified project config")
	}
	type connection struct {
		Host     string
		Port     int
		User     string
		Pass     string
		Database string
	}
	var config struct {
		Gormv2 struct{ Mysql struct{ Write connection } }
	}
	if yaml.Unmarshal(content, &config) != nil {
		t.Fatal("cannot parse project config")
	}
	c := config.Gormv2.Mysql.Write
	if c.Host == "" || c.Database == "" {
		t.Fatal("missing configured mysql target")
	}
	dsn := mysql.NewConfig()
	dsn.User = c.User
	dsn.Passwd = c.Pass
	dsn.Net = "tcp"
	dsn.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	dsn.DBName = c.Database
	dsn.Timeout = 3 * time.Second
	dsn.ReadTimeout = 3 * time.Second
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal("cannot initialize configured mysql probe")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	timings := []float64{}
	for i := 0; i < 11; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		start := time.Now()
		var one int
		err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
		ms := float64(time.Since(start).Microseconds()) / 1000
		cancel()
		if err != nil || one != 1 {
			t.Fatal("configured mysql SELECT 1 failed; connection details suppressed")
		}
		timings = append(timings, ms)
		time.Sleep(200 * time.Millisecond)
	}
	warm := append([]float64(nil), timings[1:]...)
	sort.Float64s(warm)
	result := map[string]any{"scope": "existing project MySQL via VPN; read-only SELECT 1", "connection_and_first_query_ms": timings[0], "samples_ms": timings[1:], "warm_p50_ms": warm[4], "warm_max_ms": warm[9], "includes": "driver and server turnaround, excludes MCP tool overhead; not pure network RTT"}
	b, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(filepath.Join(os.Getenv("UVP_DIRECTORY_LOAD_OUTPUT"), "vpn.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(b))
}
