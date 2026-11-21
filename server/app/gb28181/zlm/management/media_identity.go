package management

// logicalStreamCount collapses ZLM's protocol-specific views into logical
// sources. Schema is intentionally excluded because one source can expose
// RTSP, RTMP, HLS and other protocol variants at the same time.
func logicalStreamCount(streams []RuntimeMedia) int64 {
	identities := make(map[logicalStreamIdentity]struct{}, len(streams))
	for _, stream := range streams {
		identities[logicalStreamIdentity{
			nodeID: stream.NodeID,
			vhost:  stream.Media.Vhost,
			app:    stream.Media.App,
			stream: stream.Media.Stream,
		}] = struct{}{}
	}
	return int64(len(identities))
}

type logicalStreamIdentity struct {
	nodeID int64
	vhost  string
	app    string
	stream string
}
