package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type feedbackEventStore struct {
	mu     sync.Mutex
	calls  int
	events []play.LifecycleEvent
}

func (store *feedbackEventStore) AppendClientEvent(_ context.Context, _ string, _ uint, _, _ string, event play.LifecycleEvent) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls++
	store.events = append(store.events, event)
	return nil
}

func TestPlayControllerIssuesAndAcceptsClientFeedback(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	seedScopedDeviceRows(t, db)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPlayAttempt{}))
	require.NoError(t, db.Create(&gbmodels.GbPlayAttempt{
		CorrelationID: "life-feedback", UserID: 100,
		DeviceCode: "34020000002000000010", ChannelCode: "37011200001310000010",
		Outcome: "started", LifecycleState: string(play.LifecycleStateInProgress),
	}).Error)

	signer, err := playauth.NewSigner(bytes.Repeat([]byte{0x51}, 32))
	require.NoError(t, err)
	store := &feedbackEventStore{}
	service := &fixedAuthorizationControllerService{}
	beginner := &lifecycleBeginStore{id: "life-feedback"}
	controller := gbcontrollers.NewPlayController(service,
		gbcontrollers.WithPlayLifecycleBeginner(beginner),
		gbcontrollers.WithClientFeedbackRuntime(signer, store),
	)
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.POST("/api/gb28181/play/:deviceId/:channelId", controller.Start)
	router.POST("/api/gb28181/play/lifecycles/:lifecycleId/client-events", controller.ClientEvent)

	startResponse := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodPost,
		"/api/gb28181/play/34020000002000000010/37011200001310000010", nil)
	router.ServeHTTP(startResponse, startRequest)
	require.Equal(t, http.StatusOK, startResponse.Code, startResponse.Body.String())
	startData := unmarshal(t, startResponse)["data"].(map[string]any)
	token, _ := startData["clientFeedbackToken"].(string)
	require.NotEmpty(t, token)
	require.NotZero(t, startData["clientFeedbackExpiresAt"])

	feedbackResponse := httptest.NewRecorder()
	feedbackRequest := httptest.NewRequest(http.MethodPost,
		"/api/gb28181/play/lifecycles/life-feedback/client-events",
		bytes.NewBufferString(`{"event":"first_frame","clientElapsedMs":321}`))
	feedbackRequest.Header.Set("Content-Type", "application/json")
	feedbackRequest.Header.Set("X-Playback-Feedback-Token", token)
	router.ServeHTTP(feedbackResponse, feedbackRequest)
	require.Equal(t, http.StatusOK, feedbackResponse.Code, feedbackResponse.Body.String())
	require.EqualValues(t, 0, unmarshal(t, feedbackResponse)["code"])
	store.mu.Lock()
	require.Equal(t, 1, store.calls)
	require.Equal(t, play.EventFirstFrame, store.events[0].EventName)
	var metadata map[string]int64
	require.NoError(t, json.Unmarshal(store.events[0].MetadataJSON, &metadata))
	require.EqualValues(t, 321, metadata["clientElapsedMs"])
	store.mu.Unlock()
}

func TestPlayControllerRejectsSensitiveClientFeedbackFields(t *testing.T) {
	signer, err := playauth.NewSigner(bytes.Repeat([]byte{0x62}, 32))
	require.NoError(t, err)
	store := &feedbackEventStore{}
	controller := gbcontrollers.NewPlayController(&fixedAuthorizationControllerService{},
		gbcontrollers.WithClientFeedbackRuntime(signer, store))
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.POST("/api/gb28181/play/lifecycles/:lifecycleId/client-events", controller.ClientEvent)

	grant, err := signer.IssueClientFeedback(playauth.ClientFeedbackBinding{
		UserID: 100, DeviceID: "D1", ChannelID: "C1", LifecycleID: "life-1",
		AllowedEvents: []string{play.EventFirstFrame, play.EventPlayerError},
	})
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{
		"event": "player_error", "code": "player_error", "clientElapsedMs": 10,
		"url": "http://secret.example/live?token=secret",
	})
	require.NoError(t, err)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/gb28181/play/lifecycles/life-1/client-events", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Playback-Feedback-Token", grant.Token)
	router.ServeHTTP(response, request)

	require.EqualValues(t, 1, unmarshal(t, response)["code"])
	store.mu.Lock()
	require.Zero(t, store.calls)
	store.mu.Unlock()
}
