package dashboard

import (
	"fmt"
	"sort"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const highEventThreadLoad = 80

type MediaRuntimeSummary struct {
	Streams         int    `json:"streams"`
	Viewers         int    `json:"viewers"`
	NetworkSessions int64  `json:"networkSessions"`
	Recording       int    `json:"recording"`
	BytesPerSecond  uint64 `json:"bytesPerSecond"`
}

type MediaNodeHealth struct {
	Active             int     `json:"active"`
	Offline            int     `json:"offline"`
	Maintenance        int     `json:"maintenance"`
	NetThreadLoadAvg   float64 `json:"netThreadLoadAvg"`
	MaxEventThreadLoad int     `json:"maxEventThreadLoad"`
	HighLoadThreads    int     `json:"highLoadThreads"`
}

type ActiveMediaStream struct {
	Key            string   `json:"key"`
	NodeID         int64    `json:"nodeId"`
	Vhost          string   `json:"vhost"`
	App            string   `json:"app"`
	Stream         string   `json:"stream"`
	Schemas        []string `json:"schemas"`
	Viewers        int      `json:"viewers"`
	TotalReaders   int      `json:"totalReaders"`
	BytesPerSecond uint64   `json:"bytesPerSecond"`
	Recording      bool     `json:"recording"`
	AliveSecond    uint64   `json:"aliveSecond"`
}

type MediaDashboard struct {
	Runtime  MediaRuntimeSummary `json:"runtime"`
	Health   MediaNodeHealth     `json:"health"`
	Streams  []ActiveMediaStream `json:"streams"`
	Coverage Coverage            `json:"coverage"`
	AsOf     time.Time           `json:"asOf"`
}

func BuildMediaDashboard(result management.OverviewResult) MediaDashboard {
	grouped := make(map[string]*ActiveMediaStream)
	for _, stream := range result.Streams {
		if !stream.Online && stream.Media.Stream == "" {
			continue
		}
		key := mediaDashboardKey(stream)
		item := grouped[key]
		if item == nil {
			item = &ActiveMediaStream{Key: key, NodeID: stream.NodeID, Vhost: stream.Media.Vhost, App: stream.Media.App, Stream: stream.Media.Stream}
			grouped[key] = item
		}
		item.Schemas = appendUniqueSorted(item.Schemas, stream.Media.Schema)
		item.Viewers += stream.ReaderCount
		item.TotalReaders += stream.TotalReaderCount
		item.BytesPerSecond += stream.BytesSpeed
		item.Recording = item.Recording || stream.RecordingMP4 || stream.RecordingHLS
		if stream.AliveSecond > item.AliveSecond {
			item.AliveSecond = stream.AliveSecond
		}
	}

	streams := make([]ActiveMediaStream, 0, len(grouped))
	runtime := MediaRuntimeSummary{NetworkSessions: result.Metrics.NetworkSessionCount}
	for _, item := range grouped {
		streams = append(streams, *item)
		runtime.Viewers += item.Viewers
		runtime.BytesPerSecond += item.BytesPerSecond
		if item.Recording {
			runtime.Recording++
		}
	}
	runtime.Streams = len(streams)
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].Viewers != streams[j].Viewers {
			return streams[i].Viewers > streams[j].Viewers
		}
		if streams[i].BytesPerSecond != streams[j].BytesPerSecond {
			return streams[i].BytesPerSecond > streams[j].BytesPerSecond
		}
		return streams[i].Key < streams[j].Key
	})

	health := MediaNodeHealth{NetThreadLoadAvg: result.Metrics.NetThreadLoadAvg}
	for _, current := range result.Nodes {
		switch current.State {
		case node.StateActive:
			health.Active++
		case node.StateMaintenance:
			health.Maintenance++
		case node.StateOffline:
			health.Offline++
		}
		if !current.MetricsComplete {
			continue
		}
		for _, thread := range current.Metrics.EventThreadLoads {
			if thread.Load > health.MaxEventThreadLoad {
				health.MaxEventThreadLoad = thread.Load
			}
			if thread.Load >= highEventThreadLoad {
				health.HighLoadThreads++
			}
		}
	}

	coverage := CoverageComplete
	if result.Partial {
		coverage = CoveragePartial
	} else if len(result.Nodes) == 0 && len(result.Streams) == 0 {
		coverage = CoverageNotStarted
	}
	return MediaDashboard{Runtime: runtime, Health: health, Streams: streams, Coverage: coverage, AsOf: result.AsOf}
}

func mediaDashboardKey(stream management.RuntimeMedia) string {
	return fmt.Sprintf("%d\x1f%s\x1f%s\x1f%s", stream.NodeID, stream.Media.Vhost, stream.Media.App, stream.Media.Stream)
}

func appendUniqueSorted(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}
