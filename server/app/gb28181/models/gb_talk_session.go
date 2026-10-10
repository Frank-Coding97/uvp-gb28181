package models

import "time"

type TalkSessionState string

type TalkSessionMode string

type TalkSignalPhase string

const (
	TalkSessionModeBroadcast TalkSessionMode = "broadcast"
	TalkSessionModeTalk      TalkSessionMode = "talk"
)

const (
	TalkSignalPhaseSourceReady   TalkSignalPhase = "source_ready"
	TalkSignalPhaseNotifying     TalkSignalPhase = "broadcast_notifying"
	TalkSignalPhaseWaitingInvite TalkSignalPhase = "broadcast_waiting_invite"
	TalkSignalPhaseAnswering     TalkSignalPhase = "broadcast_answering"
	TalkSignalPhaseWaitingAck    TalkSignalPhase = "broadcast_waiting_ack"
	TalkSignalPhaseActive        TalkSignalPhase = "active"
)

func (m TalkSessionMode) Valid() bool {
	return m == TalkSessionModeBroadcast || m == TalkSessionModeTalk
}

const (
	TalkSessionReserved   TalkSessionState = "reserved"
	TalkSessionPublishing TalkSessionState = "publishing"
	TalkSessionInviting   TalkSessionState = "inviting"
	TalkSessionActive     TalkSessionState = "active"
	TalkSessionStopping   TalkSessionState = "stopping"
	TalkSessionEnded      TalkSessionState = "ended"
	TalkSessionFailed     TalkSessionState = "failed"
	TalkSessionExpired    TalkSessionState = "expired"
)

func (s TalkSessionState) IsTerminal() bool {
	return s == TalkSessionEnded || s == TalkSessionFailed || s == TalkSessionExpired
}

// GbTalkSession keeps one talk lease and its durable SIP/ZLM cleanup metadata.
// The nullable *Key fields enforce uniqueness only while a session is nonterminal;
// historical resource values remain available after the lease is released.
type GbTalkSession struct {
	ID                   uint64           `gorm:"primaryKey" json:"id"`
	SessionID            string           `gorm:"column:session_id;size:64;not null;uniqueIndex:uk_talk_session_session_id" json:"sessionId"`
	ChannelID            uint             `gorm:"column:channel_id;not null;index:idx_talk_session_channel_created,priority:1" json:"channelId"`
	DeviceID             string           `gorm:"column:device_id;size:20;not null" json:"deviceId"`
	TargetID             string           `gorm:"column:target_id;size:20;not null;default:'';index:idx_talk_session_broadcast_match,priority:2" json:"targetId"`
	ActorID              uint             `gorm:"column:actor_id;not null;default:0" json:"actorId"`
	ActorDeptID          uint             `gorm:"column:actor_dept_id;not null;default:0" json:"actorDeptId"`
	Mode                 TalkSessionMode  `gorm:"column:mode;size:16;not null;default:talk" json:"mode"`
	BroadcastSN          uint             `gorm:"column:broadcast_sn;not null;default:0;index:idx_talk_session_broadcast_sn" json:"-"`
	BroadcastReplyStatus string           `gorm:"column:broadcast_reply_status;size:32;not null;default:''" json:"-"`
	SignalPhase          TalkSignalPhase  `gorm:"column:signal_phase;size:40;not null;default:''" json:"phase"`
	NodeID               int64            `gorm:"column:node_id;not null;default:0;index:idx_talk_session_source,priority:1;index:idx_talk_session_recv,priority:1" json:"nodeId"`
	App                  string           `gorm:"column:app;size:64;not null;default:talk;index:idx_talk_session_source,priority:2" json:"app"`
	SourceStream         string           `gorm:"column:source_stream;size:128;not null;index:idx_talk_session_source,priority:3" json:"sourceStream"`
	RecvStream           string           `gorm:"column:recv_stream;size:128;not null;default:'';index:idx_talk_session_recv,priority:2" json:"recvStream"`
	SSRC                 string           `gorm:"column:ssrc;size:32;not null;default:''" json:"ssrc"`
	State                TalkSessionState `gorm:"column:state;size:20;not null;index:idx_talk_session_state_expires,priority:1" json:"state"`
	LeaseKey             *uint            `gorm:"column:lease_key;uniqueIndex:uk_talk_session_lease" json:"-"`
	SourceKey            *string          `gorm:"column:source_key;size:128;uniqueIndex:uk_talk_session_source_key" json:"-"`
	RecvKey              *string          `gorm:"column:recv_key;size:128;uniqueIndex:uk_talk_session_recv_key" json:"-"`
	SSRCKey              *string          `gorm:"column:ssrc_key;size:32;uniqueIndex:uk_talk_session_ssrc_key" json:"-"`
	ExpiresAt            time.Time        `gorm:"column:expires_at;not null;index:idx_talk_session_state_expires,priority:2" json:"expiresAt"`
	PublishTokenHash     string           `gorm:"column:publish_token_hash;type:char(64);not null" json:"-"`
	TokenConsumedAt      *time.Time       `gorm:"column:token_consumed_at" json:"-"`
	PublishID            string           `gorm:"column:publish_id;size:255;not null;default:''" json:"-"`
	LocalPort            int              `gorm:"column:local_port;not null;default:0" json:"-"`
	RemoteMediaIP        string           `gorm:"column:remote_media_ip;size:64;not null;default:''" json:"-"`
	RemoteMediaPort      int              `gorm:"column:remote_media_port;not null;default:0" json:"-"`
	MediaTransport       string           `gorm:"column:media_transport;size:16;not null;default:''" json:"-"`
	SenderMode           string           `gorm:"column:sender_mode;size:24;not null;default:''" json:"-"`
	CallID               string           `gorm:"column:call_id;size:255;not null;default:'';index:idx_talk_session_call_id" json:"-"`
	DialogLocalTag       string           `gorm:"column:dialog_local_tag;size:128;not null;default:''" json:"-"`
	DialogRemoteTag      string           `gorm:"column:dialog_remote_tag;size:128;not null;default:''" json:"-"`
	DialogRemoteURI      string           `gorm:"column:dialog_remote_uri;size:512;not null;default:''" json:"-"`
	DialogCSeq           uint             `gorm:"column:dialog_cseq;not null;default:0" json:"-"`
	Error                string           `gorm:"column:error;size:500;not null;default:''" json:"error"`
	StartedAt            *time.Time       `gorm:"column:started_at" json:"startedAt"`
	EndedAt              *time.Time       `gorm:"column:ended_at" json:"endedAt"`
	CreatedAt            time.Time        `gorm:"index:idx_talk_session_channel_created,priority:2" json:"createdAt"`
	UpdatedAt            time.Time        `json:"updatedAt"`
}

func (GbTalkSession) TableName() string { return "gb_talk_session" }
