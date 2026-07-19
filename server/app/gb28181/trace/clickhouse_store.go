package trace

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

var (
	ErrInvalidClickHouseIdentifier = errors.New("invalid ClickHouse identifier")
	identifierPattern              = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type clickHouseBatch interface {
	Append(...any) error
	Send() error
	Abort() error
	Close() error
}

type clickHouseRows interface {
	Next() bool
	Scan(...any) error
	Close() error
	Err() error
}

type clickHouseConn interface {
	Exec(context.Context, string, ...any) error
	PrepareBatch(context.Context, string) (clickHouseBatch, error)
	Query(context.Context, string, ...any) (clickHouseRows, error)
	Ping(context.Context) error
	Close() error
}

type nativeClickHouseConn struct {
	conn clickhousedriver.Conn
}

func (c nativeClickHouseConn) Exec(ctx context.Context, query string, args ...any) error {
	return c.conn.Exec(ctx, query, args...)
}

func (c nativeClickHouseConn) PrepareBatch(ctx context.Context, query string) (clickHouseBatch, error) {
	return c.conn.PrepareBatch(ctx, query)
}

func (c nativeClickHouseConn) Query(ctx context.Context, query string, args ...any) (clickHouseRows, error) {
	return c.conn.Query(ctx, query, args...)
}

func (c nativeClickHouseConn) Ping(ctx context.Context) error { return c.conn.Ping(ctx) }
func (c nativeClickHouseConn) Close() error                   { return c.conn.Close() }

type ClickHouseStore struct {
	conn      clickHouseConn
	database  string
	tableName string
	fullTable string
}

func OpenClickHouseStore(ctx context.Context, cfg gbconfig.TraceConfig) (*ClickHouseStore, error) {
	if strings.TrimSpace(cfg.Address) == "" || strings.TrimSpace(cfg.Username) == "" {
		return nil, fmt.Errorf("ClickHouse address and username are required")
	}
	if !identifierPattern.MatchString(cfg.Database) {
		return nil, ErrInvalidClickHouseIdentifier
	}
	password := os.Getenv(cfg.PasswordEnv)
	if cfg.PasswordEnv == "" || password == "" {
		return nil, fmt.Errorf("ClickHouse password environment variable is unavailable")
	}
	options := &clickhouse.Options{
		Protocol:    clickhouse.Native,
		Addr:        strings.Split(cfg.Address, ","),
		Auth:        clickhouse.Auth{Database: cfg.Database, Username: cfg.Username, Password: password},
		DialTimeout: 3 * time.Second,
		Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	}
	if cfg.TLS {
		options.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse trace store: %w", err)
	}
	store, err := NewClickHouseStoreWithConn(nativeClickHouseConn{conn: conn}, cfg.Database)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := store.conn.Ping(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("ping ClickHouse trace store: %w", err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func NewClickHouseStoreWithConn(conn clickHouseConn, database string) (*ClickHouseStore, error) {
	if conn == nil || !identifierPattern.MatchString(database) {
		return nil, ErrInvalidClickHouseIdentifier
	}
	const tableName = "sip_trace_message"
	return &ClickHouseStore{
		conn: conn, database: database, tableName: tableName, fullTable: database + "." + tableName,
	}, nil
}

func (s *ClickHouseStore) EnsureSchema(ctx context.Context) error {
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    event_id UUID,
    occurred_at DateTime64(6, 'UTC'),
    direction LowCardinality(String),
    transport LowCardinality(String),
    local_addr String,
    remote_addr String,
    device_id String,
    method LowCardinality(String),
    status_code UInt16,
    call_id String,
    cseq UInt32,
    cseq_method LowCardinality(String),
    malformed UInt8,
    parse_error String,
    nonce String,
    ciphertext String,
    algorithm LowCardinality(String),
    key_version LowCardinality(String),
    digest_sha256 FixedString(64),
    INDEX idx_device_id device_id TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_call_id call_id TYPE bloom_filter(0.01) GRANULARITY 4
) ENGINE = MergeTree
PARTITION BY toYYYYMMDD(occurred_at)
ORDER BY (occurred_at, event_id)
TTL occurred_at + INTERVAL 7 DAY DELETE`, s.fullTable)
	if err := s.conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("ensure ClickHouse SIP trace schema: %w", err)
	}
	summaryDDL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.sip_trace_session_day (
    day Date,
    device_id String,
    call_id String,
    first_at SimpleAggregateFunction(min, DateTime64(6, 'UTC')),
    last_at SimpleAggregateFunction(max, DateTime64(6, 'UTC')),
    message_count SimpleAggregateFunction(sum, UInt64),
    inbound_count SimpleAggregateFunction(sum, UInt64),
    outbound_count SimpleAggregateFunction(sum, UInt64),
    methods_state AggregateFunction(groupUniqArray, String),
    final_status_state AggregateFunction(argMax, UInt16, DateTime64(6, 'UTC')),
    request_count SimpleAggregateFunction(sum, UInt64),
    final_response_count SimpleAggregateFunction(sum, UInt64)
) ENGINE = AggregatingMergeTree
ORDER BY (day, device_id, call_id)
TTL day + INTERVAL 30 DAY DELETE`, s.database)
	if err := s.conn.Exec(ctx, summaryDDL); err != nil {
		return fmt.Errorf("ensure ClickHouse SIP trace session schema: %w", err)
	}
	viewDDL := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s.sip_trace_session_day_mv
TO %s.sip_trace_session_day AS
SELECT
    toDate(occurred_at) AS day,
    device_id,
    call_id,
    min(occurred_at) AS first_at,
    max(occurred_at) AS last_at,
    count() AS message_count,
    countIf(direction = 'inbound') AS inbound_count,
    countIf(direction = 'outbound') AS outbound_count,
    groupUniqArrayState(method) AS methods_state,
    argMaxState(
        if(status_code >= 200, status_code, toUInt16(0)),
        if(status_code >= 200, occurred_at, toDateTime64(0, 6, 'UTC'))
    ) AS final_status_state,
    countIf(status_code = 0 AND method NOT IN ('', 'ACK')) AS request_count,
    countIf(status_code >= 200) AS final_response_count
FROM %s
GROUP BY day, device_id, call_id`, s.database, s.database, s.fullTable)
	if err := s.conn.Exec(ctx, viewDDL); err != nil {
		return fmt.Errorf("ensure ClickHouse SIP trace session view: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) InsertBatch(ctx context.Context, events []StoredEvent) error {
	if len(events) == 0 {
		return nil
	}
	query := fmt.Sprintf(`INSERT INTO %s (
event_id, occurred_at, direction, transport, local_addr, remote_addr,
device_id, method, status_code, call_id, cseq, cseq_method,
malformed, parse_error, nonce, ciphertext, algorithm, key_version, digest_sha256
)`, s.fullTable)
	batch, err := s.conn.PrepareBatch(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare ClickHouse SIP trace batch: %w", err)
	}
	defer func() { _ = batch.Close() }()
	for _, event := range events {
		eventID, err := uuid.Parse(event.EventID)
		if err != nil {
			return fmt.Errorf("invalid SIP trace event ID")
		}
		if err := batch.Append(
			eventID, event.OccurredAt.UTC(), string(event.Direction), event.Transport,
			event.LocalAddr, event.RemoteAddr, event.DeviceID, event.Method, event.StatusCode,
			event.CallID, event.CSeq, event.CSeqMethod, boolToUInt8(event.Malformed), event.ParseError,
			event.Payload.Nonce, event.Payload.Ciphertext, event.Payload.Algorithm,
			event.Payload.KeyVersion, event.Payload.DigestSHA256,
		); err != nil {
			return fmt.Errorf("append ClickHouse SIP trace batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send ClickHouse SIP trace batch: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) Close() error { return s.conn.Close() }

func boolToUInt8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}
