package videoshare

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sync"
	"time"

	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

var ErrViewerUnavailable = errors.New("video share viewer is unavailable")

type PlaybackSource struct {
	StreamID   string
	Generation uint64
	HTTPFLVURL string
	HLSURL     string
	Release    func()
}

type PlaybackPreparer interface {
	Prepare(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error)
}

type Viewer struct {
	Token       string    `json:"viewerToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
	HTTPFLVPath string    `json:"httpFlvPath,omitempty"`
	HLSPath     string    `json:"hlsPath,omitempty"`
}

type ViewerAccess struct {
	Source     PlaybackSource
	Share      *gbmodels.GbChannelVideoShare
	ShareID    uint
	Generation string
	ExpiresAt  time.Time
	Context    context.Context
}

type viewerSession struct {
	shareID     uint
	publicID    string
	generation  string
	createdBy   uint
	devicePK    uint
	deviceID    string
	deviceEpoch uint64
	expiresAt   time.Time
	source      PlaybackSource
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	resourceMu  sync.Mutex
	resources   map[string]string
	resourceIDs map[string]string
	// Activity fields are protected by Service.viewerMu.
	activeRequests int
	idleDeadline   time.Time
	idleTimer      *time.Timer
}

func (s *Service) SetPlaybackPreparer(preparer PlaybackPreparer) {
	s.viewerMu.Lock()
	s.prepare = preparer
	s.viewerEpoch++
	s.viewerMu.Unlock()
}

// DrainViewers prevents new viewer sessions until a playback preparer is set
// again, then cancels every current session and releases its source lease.
func (s *Service) DrainViewers() {
	s.viewerMu.Lock()
	s.prepare = nil
	s.viewerEpoch++
	sessions := make([]*viewerSession, 0, len(s.viewers))
	for _, session := range s.viewers {
		if session.idleTimer != nil {
			session.idleTimer.Stop()
		}
		sessions = append(sessions, session)
	}
	s.viewers = make(map[[32]byte]*viewerSession)
	s.viewerMu.Unlock()
	for _, session := range sessions {
		closeViewerSession(session)
	}
}

func (s *Service) CreateViewer(ctx context.Context, publicID, signature string) (Viewer, error) {
	share, err := s.Resolve(ctx, publicID, signature)
	if err != nil {
		return Viewer{}, err
	}
	s.viewerMu.Lock()
	preparer := s.prepare
	epoch := s.viewerEpoch
	s.viewerMu.Unlock()
	if preparer == nil {
		return Viewer{}, ErrViewerUnavailable
	}
	token, err := s.randomID()
	if err != nil {
		return Viewer{}, err
	}
	source, err := preparer.Prepare(ctx, share)
	if err != nil {
		return Viewer{}, err
	}
	current, err := s.Resolve(ctx, publicID, signature)
	if err != nil || current.Generation != share.Generation || current.ID != share.ID {
		if source.Release != nil {
			source.Release()
		}
		if err != nil {
			return Viewer{}, err
		}
		return Viewer{}, ErrExpired
	}
	if source.StreamID == "" || source.Generation == 0 || (source.HTTPFLVURL == "" && source.HLSURL == "") {
		if source.Release != nil {
			source.Release()
		}
		return Viewer{}, fmt.Errorf("%w: playback source incomplete", ErrViewerUnavailable)
	}
	digest := sha256.Sum256([]byte(token))
	ctxWithDeadline, cancel := context.WithDeadline(context.Background(), *share.ExpiresAt)
	session := &viewerSession{
		shareID: share.ID, publicID: share.PublicID, generation: share.Generation,
		createdBy: *share.CreatedBy, devicePK: share.DevicePK, deviceID: share.DeviceID,
		deviceEpoch: share.DeviceEpoch, expiresAt: *share.ExpiresAt, source: source,
		ctx: ctxWithDeadline, cancel: cancel, resources: make(map[string]string), resourceIDs: make(map[string]string),
	}
	s.viewerMu.Lock()
	if s.viewerEpoch != epoch || s.prepare == nil {
		s.viewerMu.Unlock()
		cancel()
		if source.Release != nil {
			source.Release()
		}
		return Viewer{}, ErrViewerUnavailable
	}
	if s.viewers == nil {
		s.viewers = make(map[[32]byte]*viewerSession)
	}
	s.viewers[digest] = session
	s.armViewerIdleLocked(digest, session)
	s.viewerMu.Unlock()
	go func() {
		<-session.ctx.Done()
		_ = s.CloseViewer(token)
	}()
	viewer := Viewer{Token: token, ExpiresAt: *share.ExpiresAt}
	if source.HTTPFLVURL != "" {
		viewer.HTTPFLVPath = "/api/public/video-share/viewers/" + token + "/live.flv"
	}
	if source.HLSURL != "" {
		viewer.HLSPath = "/api/public/video-share/viewers/" + token + "/hls/index.m3u8"
	}
	return viewer, nil
}

func (s *Service) LookupViewer(ctx context.Context, token string) (ViewerAccess, error) {
	if token == "" {
		return ViewerAccess{}, ErrExpired
	}
	digest := sha256.Sum256([]byte(token))
	s.viewerMu.Lock()
	session := s.viewers[digest]
	s.viewerMu.Unlock()
	if session == nil || !session.expiresAt.After(s.now()) || session.ctx.Err() != nil {
		_ = s.CloseViewer(token)
		return ViewerAccess{}, ErrExpired
	}
	var currentShare gbmodels.GbChannelVideoShare
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var share gbmodels.GbChannelVideoShare
		if err := tx.Where("id = ?", session.shareID).Take(&share).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExpired
			}
			return err
		}
		if share.PublicID != session.publicID || share.Generation != session.generation || !share.Enabled || share.RevokedAt != nil || share.ExpiresAt == nil || !share.ExpiresAt.After(s.now()) || !share.ExpiresAt.Equal(session.expiresAt) || share.CreatedBy == nil || *share.CreatedBy != session.createdBy {
			return ErrExpired
		}
		snapshot, err := s.authorize(ctx, tx, session.createdBy, share.ChannelPK)
		if err != nil {
			return err
		}
		if !matchesDevice(snapshot, session.devicePK, session.deviceID, session.deviceEpoch) || !matchesDevice(snapshot, share.DevicePK, share.DeviceID, share.DeviceEpoch) {
			return ErrExpired
		}
		currentShare = share
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrExpired) {
			_ = s.CloseViewer(token)
		} else {
			err = fmt.Errorf("%w: authorization check failed", ErrViewerUnavailable)
		}
		return ViewerAccess{}, err
	}
	return ViewerAccess{Source: session.source, Share: &currentShare, ShareID: session.shareID, Generation: session.generation, ExpiresAt: session.expiresAt, Context: session.ctx}, nil
}

func (s *Service) viewerSession(token string) *viewerSession {
	digest := sha256.Sum256([]byte(token))
	s.viewerMu.Lock()
	defer s.viewerMu.Unlock()
	return s.viewers[digest]
}

func (s *Service) mapHLSResource(token, resourcePath string) (string, error) {
	session := s.viewerSession(token)
	if session == nil {
		return "", ErrExpired
	}
	session.resourceMu.Lock()
	defer session.resourceMu.Unlock()
	if existing := session.resourceIDs[resourcePath]; existing != "" {
		return existing, nil
	}
	if len(session.resources) >= 4096 {
		return "", ErrViewerUnavailable
	}
	resourceID, err := s.randomID()
	if err != nil {
		return "", err
	}
	session.resources[resourceID] = resourcePath
	session.resourceIDs[resourcePath] = resourceID
	return resourceID, nil
}

func (s *Service) resolveHLSResource(token, resourceID string) (string, bool) {
	session := s.viewerSession(token)
	if session == nil {
		return "", false
	}
	session.resourceMu.Lock()
	defer session.resourceMu.Unlock()
	resourcePath, ok := session.resources[resourceID]
	return resourcePath, ok
}

func hlsResourcePrefix(playlistURL *url.URL) string {
	return path.Clean(path.Dir(playlistURL.Path)) + "/"
}

func (s *Service) CloseViewer(token string) error {
	if token == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(token))
	s.viewerMu.Lock()
	session := s.viewers[digest]
	delete(s.viewers, digest)
	if session != nil && session.idleTimer != nil {
		session.idleTimer.Stop()
	}
	s.viewerMu.Unlock()
	if session == nil {
		return nil
	}
	closeViewerSession(session)
	return nil
}

// beginViewerRequest keeps the source lease while media is in flight. Status
// polling deliberately does not count as media activity.
func (s *Service) beginViewerRequest(token string) (func(bool), error) {
	digest := sha256.Sum256([]byte(token))
	s.viewerMu.Lock()
	session := s.viewers[digest]
	if session == nil || session.ctx.Err() != nil {
		s.viewerMu.Unlock()
		return nil, ErrExpired
	}
	session.activeRequests++
	if session.idleTimer != nil {
		session.idleTimer.Stop()
	}
	s.viewerMu.Unlock()
	return func(closeOnFinish bool) {
		s.viewerMu.Lock()
		if s.viewers[digest] != session {
			s.viewerMu.Unlock()
			return
		}
		session.activeRequests--
		closeNow := closeOnFinish && session.activeRequests == 0
		if closeNow {
			delete(s.viewers, digest)
		} else if session.activeRequests == 0 {
			s.armViewerIdleLocked(digest, session)
		}
		s.viewerMu.Unlock()
		if closeNow {
			closeViewerSession(session)
		}
	}, nil
}

func (s *Service) armViewerIdleLocked(digest [32]byte, session *viewerSession) {
	timeout := s.viewerIdleTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	session.idleDeadline = time.Now().Add(timeout)
	if session.idleTimer != nil {
		session.idleTimer.Stop()
	}
	session.idleTimer = time.AfterFunc(timeout, func() {
		s.viewerMu.Lock()
		// A stopped callback may already be waiting for this lock. Recheck
		// the latest deadline so it cannot retire a newly active viewer.
		if s.viewers[digest] != session || session.activeRequests != 0 || time.Now().Before(session.idleDeadline) {
			s.viewerMu.Unlock()
			return
		}
		delete(s.viewers, digest)
		s.viewerMu.Unlock()
		closeViewerSession(session)
	})
}

func closeViewerSession(session *viewerSession) {
	if session == nil {
		return
	}
	session.closeOnce.Do(func() {
		session.cancel()
		if session.source.Release != nil {
			session.source.Release()
		}
	})
}
