package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"sort"
	"strconv"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	zlmservice "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

var (
	errNodeImpactMediaUnavailable    = errors.New("media impact evidence unavailable")
	errNodeImpactSessionsUnavailable = errors.New("session impact evidence unavailable")
)

// NodeImpactSnapshotReader is a deliberately fresh, typed runtime boundary.
// Implementations must not serve cached values: preflight and execution use
// separate calls so a changed target set invalidates the confirmation digest.
type NodeImpactSnapshotReader interface {
	GetMediaListFresh(context.Context, int64) ([]zlm.MediaInfo, error)
	GetAllSessionsFresh(context.Context, int64) ([]zlm.Session, error)
}

// ExecutorNodeImpactSnapshotReader binds the two exact-set reads to the same
// node-aware executor used by the rest of the management facade.
type ExecutorNodeImpactSnapshotReader struct {
	executor *NodeExecutor
}

func NewExecutorNodeImpactSnapshotReader(executor *NodeExecutor) *ExecutorNodeImpactSnapshotReader {
	return &ExecutorNodeImpactSnapshotReader{executor: executor}
}

func (r *ExecutorNodeImpactSnapshotReader) GetMediaListFresh(ctx context.Context, nodeID int64) ([]zlm.MediaInfo, error) {
	if r == nil || r.executor == nil {
		return nil, zlmservice.ErrNodeImpactProviderUnavailable
	}
	var media []zlm.MediaInfo
	if err := r.executor.ExecuteRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		media, err = client.GetMediaListFiltered(operationCtx, zlm.MediaFilter{})
		return err
	}); err != nil {
		return nil, err
	}
	return media, nil
}

func (r *ExecutorNodeImpactSnapshotReader) GetAllSessionsFresh(ctx context.Context, nodeID int64) ([]zlm.Session, error) {
	if r == nil || r.executor == nil {
		return nil, zlmservice.ErrNodeImpactProviderUnavailable
	}
	var sessions []zlm.Session
	if err := r.executor.ExecuteRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		sessions, err = client.GetAllSessions(operationCtx, zlm.SessionFilter{})
		return err
	}); err != nil {
		return nil, err
	}
	return sessions, nil
}

// RuntimeNodeImpactProvider derives the bounded UI counts and an exact-set
// digest from live ZLM media/session data. Recording state is part of each
// media identity record, so equal counts with different targets never share a
// confirmation fingerprint.
type RuntimeNodeImpactProvider struct {
	reader NodeImpactSnapshotReader
}

func NewRuntimeNodeImpactProvider(reader NodeImpactSnapshotReader) *RuntimeNodeImpactProvider {
	return &RuntimeNodeImpactProvider{reader: reader}
}

func (p *RuntimeNodeImpactProvider) ReadNodeImpact(ctx context.Context, current *node.Node) (zlmservice.NodeImpact, error) {
	if p == nil || p.reader == nil {
		return zlmservice.NodeImpact{}, zlmservice.ErrNodeImpactProviderUnavailable
	}
	if current == nil || current.ID <= 0 {
		return zlmservice.NodeImpact{}, zlmservice.ErrNodeImpactInvalid
	}
	media, err := p.reader.GetMediaListFresh(ctx, current.ID)
	if err != nil {
		return zlmservice.NodeImpact{}, errNodeImpactMediaUnavailable
	}
	sessions, err := p.reader.GetAllSessionsFresh(ctx, current.ID)
	if err != nil {
		return zlmservice.NodeImpact{}, errNodeImpactSessionsUnavailable
	}

	recordings := 0
	records := make([]string, 0, len(media)+len(sessions))
	for _, item := range media {
		if item.IsRecordingMP4 {
			recordings++
		}
		if item.IsRecordingHLS {
			recordings++
		}
		records = append(records, canonicalImpactRecord(
			"media", item.Schema, item.VHost, item.App, item.Stream,
			strconv.FormatBool(item.IsRecordingMP4), strconv.FormatBool(item.IsRecordingHLS),
		))
	}
	for _, item := range sessions {
		records = append(records, canonicalImpactRecord(
			"session", item.ID, item.Identifier, item.Type, item.TypeID,
			item.PeerIP, strconv.Itoa(item.PeerPort), item.LocalIP, strconv.Itoa(item.LocalPort),
		))
	}
	sort.Strings(records)

	return zlmservice.NodeImpact{
		Streams:             len(media),
		Recordings:          recordings,
		Sessions:            len(sessions),
		Truncated:           len(media) > zlmservice.MaxNodeImpactItems || recordings > zlmservice.MaxNodeImpactItems || len(sessions) > zlmservice.MaxNodeImpactItems,
		EvidenceFingerprint: hashImpactRecords(records),
	}, nil
}

func canonicalImpactRecord(kind string, fields ...string) string {
	h := sha256.New()
	writeImpactField(h, kind)
	for _, field := range fields {
		writeImpactField(h, field)
	}
	return kind + ":" + hex.EncodeToString(h.Sum(nil))
}

func hashImpactRecords(records []string) string {
	h := sha256.New()
	writeImpactField(h, "zlm-node-impact-v1")
	for _, record := range records {
		writeImpactField(h, record)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeImpactField(h hash.Hash, value string) {
	_, _ = h.Write([]byte(strconv.Itoa(len(value))))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{'|'})
}

var _ zlmservice.NodeImpactProvider = (*RuntimeNodeImpactProvider)(nil)
var _ NodeImpactSnapshotReader = (*ExecutorNodeImpactSnapshotReader)(nil)
