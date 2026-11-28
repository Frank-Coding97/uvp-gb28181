//go:build darwin || linux

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

type directoryLoadSQL struct {
	logger.Interface
	queries atomic.Int64
	rows    atomic.Int64
}

func (l *directoryLoadSQL) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	_, rows := fc()
	l.queries.Add(1)
	if rows > 0 {
		l.rows.Add(rows)
	}
}

type directoryLoadSample struct {
	MS       float64
	Bytes    int
	Revision string
	Reset    bool
	Upserts  int
	Code     int
}
type directoryLoadResult struct {
	Devices            int     `json:"devices"`
	Case               string  `json:"case"`
	Clients            int     `json:"clients"`
	Requests           int     `json:"requests"`
	P50MS              float64 `json:"p50_ms"`
	P95MS              float64 `json:"p95_ms"`
	MaxMS              float64 `json:"max_ms"`
	BytesMean          float64 `json:"bytes_mean"`
	SQLPerRequest      float64 `json:"sql_per_request"`
	RowsPerRequest     float64 `json:"rows_per_request"`
	AllocMiBPerRequest float64 `json:"alloc_mib_per_request"`
	PeakHeapMiB        float64 `json:"peak_heap_mib"`
	CPUCorePercent     float64 `json:"cpu_core_percent"`
	Errors             int     `json:"errors"`
	Resets             int     `json:"resets"`
	Skipped            string  `json:"skipped,omitempty"`
}

func directoryLoadCPU() float64 {
	var r syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
}

// TestDirectoryLoad is opt-in. All synthetic writes use a disposable local SQLite DB.
// It never reads application config or connects to the configured remote database.
func TestDirectoryLoad(t *testing.T) {
	if os.Getenv("UVP_DIRECTORY_LOAD") != "1" {
		t.Skip("set UVP_DIRECTORY_LOAD=1 for isolated load test")
	}
	gin.SetMode(gin.ReleaseMode)
	app.Response = response.NewResponseHandler()
	out := os.Getenv("UVP_DIRECTORY_LOAD_OUTPUT")
	require.NotEmpty(t, out)
	require.NoError(t, os.MkdirAll(out, 0700))
	sizes := []int{1000, 10000, 50000}
	if value := os.Getenv("UVP_DIRECTORY_LOAD_SIZES"); value != "" {
		sizes = nil
		for _, v := range strings.Split(value, ",") {
			n, e := strconv.Atoi(v)
			require.NoError(t, e)
			sizes = append(sizes, n)
		}
	}
	results := []directoryLoadResult{}
	save := func(r directoryLoadResult) {
		results = append(results, r)
		b, _ := json.MarshalIndent(results, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(out, "backend.json"), b, 0600))
		t.Logf("LOAD_RESULT %+v", r)
	}
	for _, n := range sizes {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			metrics := &directoryLoadSQL{Interface: logger.Discard}
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "load.sqlite")), &gorm.Config{Logger: metrics})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			defer sqlDB.Close()
			sqlDB.SetMaxOpenConns(50)
			require.NoError(t, db.Exec("PRAGMA journal_mode=WAL").Error)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbCatalogNode{}, &gbmodels.GbAlarmResource{}, &gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}, &civilcode.SysCivilCode{}, &basemodels.SysDepartment{}))
			require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "压测部门"}).Error)
			require.NoError(t, db.Create(&[]civilcode.SysCivilCode{{Code: "370000", Name: "山东省", ShortName: "山东省", Level: 1}, {Code: "370100", Name: "济南市", ShortName: "济南市", ParentCode: "370000", Level: 2}, {Code: "370103", Name: "市中区", ShortName: "市中区", ParentCode: "370100", Level: 3}}).Error)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				for start := 1; start <= n; start += 100 {
					devices := []gbmodels.GbDevice{}
					channels := []gbmodels.GbChannel{}
					for i := start; i < start+100 && i <= n; i++ {
						code := fmt.Sprintf("3701030102118%07d", i)
						devices = append(devices, gbmodels.GbDevice{BaseModel: basemodels.BaseModel{ID: uint(i)}, DeviceID: code, Name: fmt.Sprintf("压测设备-%06d", i), OwnerDeptID: 10, Status: int8(i % 2)})
						channels = append(channels, gbmodels.GbChannel{ID: uint(i), DeviceID: code, ChannelID: fmt.Sprintf("3701030102132%07d", i), Name: fmt.Sprintf("压测通道-%06d", i), OwnerDeptID: 10, Status: int8(i % 2)})
					}
					if err := tx.Select("id", "device_id", "name", "alias", "owner_dept_id", "status").Create(&devices).Error; err != nil {
						return err
					}
					if err := tx.Select("id", "device_id", "channel_id", "name", "owner_dept_id", "status").Create(&channels).Error; err != nil {
						return err
					}
				}
				return nil
			}))
			router := gin.New()
			router.GET("/tree", NewDirectoryController(func() *gorm.DB { return db }).Tree)
			request := func(query string, fixture bool) directoryLoadSample {
				w := httptest.NewRecorder()
				req := httptest.NewRequest("GET", "/tree?view=national&sync=1"+query, nil)
				start := time.Now()
				router.ServeHTTP(w, req)
				elapsed := float64(time.Since(start).Microseconds()) / 1000
				var payload struct {
					Code int `json:"code"`
					Data struct {
						Revision string            `json:"revision"`
						Reset    bool              `json:"reset"`
						Upserts  []json.RawMessage `json:"upserts"`
					} `json:"data"`
				}
				e := json.Unmarshal(w.Body.Bytes(), &payload)
				code := w.Code
				if e != nil || payload.Code != 0 || payload.Data.Revision == "" {
					code = 500
				}
				if fixture && code == 200 {
					_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("tree-%d.json", n)), w.Body.Bytes(), 0600)
				}
				return directoryLoadSample{elapsed, w.Body.Len(), payload.Data.Revision, payload.Data.Reset, len(payload.Data.Upserts), code}
			}
			run := func(name string, clients, count int, query string, before func(int), cadence bool) directoryLoadResult {
				runtime.GC()
				var initial runtime.MemStats
				runtime.ReadMemStats(&initial)
				q0, r0 := metrics.queries.Load(), metrics.rows.Load()
				cpu0 := directoryLoadCPU()
				start := time.Now()
				var peak atomic.Uint64
				peak.Store(initial.HeapAlloc)
				done := make(chan struct{})
				var monitor sync.WaitGroup
				monitor.Add(1)
				go func() {
					defer monitor.Done()
					tick := time.NewTicker(25 * time.Millisecond)
					defer tick.Stop()
					for {
						select {
						case <-done:
							return
						case <-tick.C:
							var m runtime.MemStats
							runtime.ReadMemStats(&m)
							if m.HeapAlloc > peak.Load() {
								peak.Store(m.HeapAlloc)
							}
						}
					}
				}()
				samples := make([]directoryLoadSample, clients*count)
				for round := 0; round < count; round++ {
					if cadence && round > 0 {
						if wait := 10*time.Second - time.Since(start); wait > 0 {
							time.Sleep(wait)
						}
					}
					if before != nil {
						before(round)
					}
					var wg sync.WaitGroup
					for client := 0; client < clients; client++ {
						wg.Add(1)
						go func(idx int) { defer wg.Done(); samples[idx] = request(query, false) }(round*clients + client)
					}
					wg.Wait()
					// Match the browser: each next poll sends the latest acknowledged revision.
					if strings.HasPrefix(query, "&since=") && samples[round*clients].Revision != "" {
						query = "&since=" + samples[round*clients].Revision
					}
				}
				close(done)
				monitor.Wait()
				wall := time.Since(start).Seconds()
				cpu := directoryLoadCPU() - cpu0
				var end runtime.MemStats
				runtime.ReadMemStats(&end)
				times := []float64{}
				var bytes float64
				errors, resets := 0, 0
				for _, s := range samples {
					times = append(times, s.MS)
					bytes += float64(s.Bytes)
					if s.Code != 200 {
						errors++
					}
					if s.Reset {
						resets++
					}
				}
				sort.Float64s(times)
				total := float64(len(samples))
				result := directoryLoadResult{Devices: n, Case: name, Clients: clients, Requests: len(samples), P50MS: times[(len(times)-1)/2], P95MS: times[int(math.Ceil(total*.95))-1], MaxMS: times[len(times)-1], BytesMean: bytes / total, SQLPerRequest: float64(metrics.queries.Load()-q0) / total, RowsPerRequest: float64(metrics.rows.Load()-r0) / total, AllocMiBPerRequest: float64(end.TotalAlloc-initial.TotalAlloc) / 1048576 / total, PeakHeapMiB: float64(peak.Load()) / 1048576, CPUCorePercent: 100 * cpu / wall, Errors: errors, Resets: resets}
				save(result)
				return result
			}
			first := request("", true)
			require.Equal(t, 200, first.Code)
			save(directoryLoadResult{Devices: n, Case: "first_full", Clients: 1, Requests: 1, P50MS: first.MS, P95MS: first.MS, MaxMS: first.MS, BytesMean: float64(first.Bytes), Resets: 1})
			baseline := request("&since="+first.Revision, false)
			require.False(t, baseline.Reset)
			require.Zero(t, baseline.Upserts)
			unchanged := run("unchanged", 1, 5, "&since="+first.Revision, nil, false)
			run("status_change_1pct", 1, 3, "&since="+first.Revision, func(_ int) {
				require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id <= ?", n/100).Update("status", gorm.Expr("1 - status")).Error)
			}, false)
			run("filter_device_name", 1, 3, "&assetKind=device&q=000001", nil, false)
			run("filter_channel_online", 1, 3, "&assetKind=channel&q=000001&status=online", nil, false)
			current := request("", false)
			for _, clients := range []int{1, 10, 50} {
				// Conservative allocation-based guard protects the user's interactive workstation.
				if unchanged.AllocMiBPerRequest*float64(clients) > 768 || (unchanged.P95MS > 10000 && clients > 1) {
					save(directoryLoadResult{Devices: n, Case: "poll_10s", Clients: clients, Skipped: fmt.Sprintf("guard: %.1f MiB allocated/request * %d clients exceeds 768 MiB, or single request exceeds refresh interval", unchanged.AllocMiBPerRequest, clients)})
					continue
				}
				run("poll_10s", clients, 2, "&since="+current.Revision, nil, true)
			}
		})
	}
}
