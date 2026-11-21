package talk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestOpenAPIClientSessionLookupIsIsolatedAndPersisted(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	firstKey := "request-1"
	secondKey := "request-1"
	first := newReservedSession("openapi-client-31", 501, now.Add(time.Minute))
	first.OpenAPIClientID = 31
	first.OpenAPIIdempotencyKey = &firstKey
	first.OpenAPIRequestHash = strings.Repeat("a", 64)
	second := newReservedSession("openapi-client-32", 502, now.Add(time.Minute))
	second.OpenAPIClientID = 32
	second.OpenAPIIdempotencyKey = &secondKey
	second.OpenAPIRequestHash = strings.Repeat("b", 64)
	uiSession := newReservedSession("interactive-user", 503, now.Add(time.Minute))

	require.NoError(t, repo.Create(ctx, first, "token-first"))
	require.NoError(t, repo.Create(ctx, second, "token-second"))
	require.NoError(t, repo.Create(ctx, uiSession, "token-ui"))

	byFirstClient, err := repo.FindByOpenAPIClient(ctx, 31, first.SessionID)
	require.NoError(t, err)
	require.Equal(t, first.SessionID, byFirstClient.SessionID)
	otherClient, err := repo.FindByOpenAPIClient(ctx, 32, first.SessionID)
	require.NoError(t, err)
	require.Nil(t, otherClient)
	interactive, err := repo.FindByOpenAPIClient(ctx, 31, uiSession.SessionID)
	require.NoError(t, err)
	require.Nil(t, interactive)

	firstRetry, err := repo.FindByOpenAPIIdempotency(ctx, 31, "request-1")
	require.NoError(t, err)
	require.Equal(t, first.SessionID, firstRetry.SessionID)
	secondClientKey, err := repo.FindByOpenAPIIdempotency(ctx, 32, "request-1")
	require.NoError(t, err)
	require.Equal(t, second.SessionID, secondClientKey.SessionID)

	var persisted models.GbTalkSession
	require.NoError(t, db.Where("session_id = ?", first.SessionID).Take(&persisted).Error)
	require.EqualValues(t, 31, persisted.OpenAPIClientID)
	require.NotNil(t, persisted.OpenAPIIdempotencyKey)
	require.Equal(t, "request-1", *persisted.OpenAPIIdempotencyKey)
}

func TestOpenAPIIdempotencyLookupIsClientScopedAndPayloadBound(t *testing.T) {
	db := newTalkRepoTestDB(t)
	repo := NewGormRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	key := "same-key"
	first := newReservedSession("idem-client-31", 601, now.Add(time.Minute))
	first.OpenAPIClientID, first.OpenAPIIdempotencyKey, first.OpenAPIRequestHash = 31, &key, strings.Repeat("a", 64)
	require.NoError(t, repo.Create(ctx, first, "token-first"))

	retry := newReservedSession("idem-retry", 602, now.Add(time.Minute))
	retry.OpenAPIClientID, retry.OpenAPIIdempotencyKey, retry.OpenAPIRequestHash = 31, &key, strings.Repeat("a", 64)
	require.NoError(t, repo.Create(ctx, retry, "token-retry"))
	require.Equal(t, first.SessionID, retry.SessionID, "retry returns the original durable session")

	conflict := newReservedSession("idem-conflict", 603, now.Add(time.Minute))
	conflict.OpenAPIClientID, conflict.OpenAPIIdempotencyKey, conflict.OpenAPIRequestHash = 31, &key, strings.Repeat("b", 64)
	require.ErrorIs(t, repo.Create(ctx, conflict, "token-conflict"), ErrOpenAPIIdempotencyConflict)

	otherClient := newReservedSession("idem-client-32", 604, now.Add(time.Minute))
	otherClient.OpenAPIClientID, otherClient.OpenAPIIdempotencyKey, otherClient.OpenAPIRequestHash = 32, &key, strings.Repeat("a", 64)
	require.NoError(t, repo.Create(ctx, otherClient, "token-other"))
	require.NotEqual(t, first.SessionID, otherClient.SessionID)

	var count int64
	require.NoError(t, db.Model(&models.GbTalkSession{}).Count(&count).Error)
	require.EqualValues(t, 2, count, "an idempotent retry must not persist a second session")
}
