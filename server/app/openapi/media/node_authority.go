package media

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// NodeAuthority is the transaction-local durable runtime recheck used by the
// OpenAPI grant/viewer boundary. It is intentionally stateless: the caller's
// supplied transaction is the only source of node state. This is a current
// runtime identity check, not deployment or topology qualification; the root
// must still refuse startup until a real qualified-node provider is present.
type NodeAuthority struct{}

var _ playauth.OpenAPINodeAuthority = (*NodeAuthority)(nil)

// NewNodeAuthority constructs the SQL-only runtime authority. It does not own
// a database handle and therefore cannot fall back to a global connection.
func NewNodeAuthority() *NodeAuthority {
	return &NodeAuthority{}
}

// nodeAuthorityProjection deliberately uses pointers so NULL values and
// malformed legacy rows fail closed instead of becoming zero-value evidence.
// The state column has no soft-delete companion in the current meta_node
// schema: state=active is the live-row condition and physical deletion is a
// zero-row failure.
type nodeAuthorityProjection struct {
	ID                       *int64     `gorm:"column:id"`
	Revision                 *uint64    `gorm:"column:revision"`
	State                    *string    `gorm:"column:state"`
	MediaServerUUID          *string    `gorm:"column:media_server_uuid"`
	CurrentBootNonce         *string    `gorm:"column:current_boot_nonce"`
	RuntimeEpoch             *int64     `gorm:"column:runtime_epoch"`
	RuntimeProtocolVersion   *int64     `gorm:"column:runtime_protocol_version"`
	RuntimeConfirmedRevision *int64     `gorm:"column:runtime_confirmed_revision"`
	RuntimeConfirmedAt       *time.Time `gorm:"column:runtime_confirmed_at"`
	RuntimeIdentityStatus    *string    `gorm:"column:runtime_identity_status"`
}

// AuthorizeOpenAPI rechecks one exact node/runtime tuple in the transaction
// supplied by the grant or viewer service. It never starts a transaction,
// opens another database, consults a cache, or performs network I/O.
func (a *NodeAuthority) AuthorizeOpenAPI(ctx context.Context, tx *gorm.DB, request playauth.OpenAPINodeAuthorization) error {
	if a == nil || ctx == nil || ctx.Err() != nil || tx == nil || !validNodeAuthorityInput(request) {
		return playauth.ErrOpenAPIGrantUnavailable
	}

	var rows []nodeAuthorityProjection
	result := tx.WithContext(ctx).
		Table((models.MediaNodeSecurity{}).TableName()).
		Select("id, revision, state, media_server_uuid, current_boot_nonce, runtime_epoch, runtime_protocol_version, runtime_confirmed_revision, runtime_confirmed_at, runtime_identity_status").
		Where("media_server_uuid = ?", request.NodeUUID).
		Limit(2).
		Find(&rows)
	if result.Error != nil || result.RowsAffected != 1 || len(rows) != 1 {
		return playauth.ErrOpenAPIGrantUnavailable
	}
	if !validNodeAuthorityProjection(rows[0], request) {
		return playauth.ErrOpenAPIGrantUnavailable
	}
	return nil
}

func validNodeAuthorityInput(request playauth.OpenAPINodeAuthorization) bool {
	if !validNodeAuthorityNodeUUID(request.NodeUUID) || !validNodeAuthorityBootNonce(request.BootNonce) {
		return false
	}
	return request.Protocol == "https-flv" || request.Protocol == "wss-flv"
}

func validNodeAuthorityNodeUUID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validNodeAuthorityBootNonce(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validNodeAuthorityProjection(row nodeAuthorityProjection, request playauth.OpenAPINodeAuthorization) bool {
	if row.ID == nil || *row.ID <= 0 || row.Revision == nil || *row.Revision == 0 || row.State == nil || *row.State != string(node.StateActive) || row.MediaServerUUID == nil || *row.MediaServerUUID != request.NodeUUID || row.CurrentBootNonce == nil || *row.CurrentBootNonce != request.BootNonce || row.RuntimeEpoch == nil || *row.RuntimeEpoch <= 0 || row.RuntimeProtocolVersion == nil || *row.RuntimeProtocolVersion != config.NodeRuntimeProtocolV1 || row.RuntimeConfirmedRevision == nil || *row.RuntimeConfirmedRevision <= 0 || uint64(*row.RuntimeConfirmedRevision) != *row.Revision || row.RuntimeConfirmedAt == nil || row.RuntimeConfirmedAt.IsZero() || row.RuntimeIdentityStatus == nil || *row.RuntimeIdentityStatus != config.NodeRuntimeStatusActive {
		return false
	}
	return true
}
