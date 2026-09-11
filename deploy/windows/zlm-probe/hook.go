package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	hookOnServerStarted    = "on_server_started"
	hookOnServerKeepalive  = "on_server_keepalive"
	hookOnStreamChanged    = "on_stream_changed"
	hookOnStreamNoneReader = "on_stream_none_reader"
	hookOnRTPServerTimeout = "on_rtp_server_timeout"
	hookOnPublish          = "on_publish"
	hookOnPlay             = "on_play"
	hookOnFlowReport       = "on_flow_report"
	hookOnStreamNotFound   = "on_stream_not_found"
	hookOnRecordMP4        = "on_record_mp4"
	hookRequestBodyLimit   = 1 << 20
	hookResponseWriteLimit = 1 << 10
)

var managedHookEvents = []string{
	hookOnServerStarted,
	hookOnServerKeepalive,
	hookOnStreamChanged,
	hookOnStreamNoneReader,
	hookOnRTPServerTimeout,
	hookOnPublish,
	hookOnPlay,
	hookOnFlowReport,
	hookOnStreamNotFound,
	hookOnRecordMP4,
}

type hookObservation struct {
	event        string
	responseCode int
	hookIndex    uint64
	close        *bool
	when         time.Time
}

type hookValidationFailure struct {
	event  string
	fields []string
}

type hookReceiver struct {
	server       *http.Server
	listener     net.Listener
	baseURL      string
	node         string
	secret       string
	playToken    string
	publishToken string

	mu           sync.Mutex
	observations []hookObservation
	validation   []hookValidationFailure
}

func newHookReceiver(node, secret string) (*hookReceiver, error) {
	if strings.TrimSpace(node) == "" || strings.TrimSpace(secret) == "" {
		return nil, errors.New("hook receiver credentials are empty")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("hook receiver could not bind a local port")
	}
	receiver := &hookReceiver{
		server:   &http.Server{ReadHeaderTimeout: 3 * time.Second},
		listener: listener,
		baseURL:  "http://" + listener.Addr().String(),
		node:     node,
		secret:   secret,
	}
	receiver.server.Handler = http.HandlerFunc(receiver.handle)
	go func() {
		_ = receiver.server.Serve(listener)
	}()
	return receiver, nil
}

func (receiver *hookReceiver) close(timeout time.Duration) error {
	if receiver == nil || receiver.server == nil {
		return nil
	}
	ctx, cancel := contextWithTimeout(timeout)
	defer cancel()
	err := receiver.server.Shutdown(ctx)
	if err != nil {
		_ = receiver.listener.Close()
	}
	return err
}

// contextWithTimeout is kept local so the probe's process cleanup stays bounded
// without requiring callers to know anything about the receiver implementation.
func contextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func (receiver *hookReceiver) handle(writer http.ResponseWriter, request *http.Request) {
	event, eventOK := hookEventFromPath(request.URL.Path)
	body, bodyOK := readHookBody(request)
	valid, fields := receiver.validateRequest(request, event, body)
	authOK := eventOK && request.Method == http.MethodPost && bodyOK && valid
	if !authOK {
		receiver.recordValidationFailure(event, fields)
		writeHookResponse(writer, hookResponse{Code: -1, Msg: "hook authorization denied"})
		return
	}

	response := receiver.policy(event, body)
	var observation hookObservation
	observation.event = event
	observation.responseCode = response.Code
	observation.when = time.Now().UTC()
	if raw := body["hook_index"]; len(raw) != 0 {
		observation.hookIndex, _ = parseJSONUint(raw)
	}
	if event == hookOnStreamNoneReader {
		close := response.Close
		observation.close = &close
	}
	receiver.mu.Lock()
	receiver.observations = append(receiver.observations, observation)
	receiver.mu.Unlock()
	writeHookResponse(writer, response)
}

func (receiver *hookReceiver) validateRequest(request *http.Request, event string, body map[string]json.RawMessage) (bool, []string) {
	fields := make([]string, 0)
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		fields = append(fields, "content-type")
	}
	query := request.URL.Query()
	nodes, nodesOK := query["node"]
	caps, capsOK := query["cap"]
	if !nodesOK || len(nodes) != 1 || nodes[0] != receiver.node || !capsOK || len(caps) != 1 || caps[0] == "" {
		fields = append(fields, "node/cap")
	} else if !hmac.Equal([]byte(caps[0]), []byte(hookCapability(receiver.secret, receiver.node, event))) {
		fields = append(fields, "cap")
	}
	if event == "" {
		fields = append(fields, "event")
	} else if _, known := hookEventFromPath("/index/hook/" + event); !known {
		fields = append(fields, "event")
	}
	if body == nil {
		fields = append(fields, "json-body")
		return false, fields
	}
	mediaServerID, mediaServerOK := rawString(body["mediaServerId"])
	if !mediaServerOK || mediaServerID != receiver.node {
		fields = append(fields, "mediaServerId")
	}
	if _, indexOK := parseJSONUint(body["hook_index"]); !indexOK {
		fields = append(fields, "hook_index")
	}
	if vhost, vhostOK := rawString(body["vhost"]); vhostOK && vhost != "" && request.Header.Get("X-VHOST") != vhost {
		fields = append(fields, "X-VHOST")
	}
	fields = append(fields, validateHookPayload(event, body)...)
	return len(fields) == 0, fields
}

func validateHookPayload(event string, body map[string]json.RawMessage) []string {
	missing := make([]string, 0)
	requireString := func(name string, nonEmpty bool) {
		value, ok := rawString(body[name])
		if !ok || (nonEmpty && strings.TrimSpace(value) == "") {
			missing = append(missing, name)
		}
	}
	requireNumber := func(name string) {
		if !jsonNumber(body[name]) {
			missing = append(missing, name)
		}
	}
	requireUint := func(name string) {
		if !jsonUnsignedNumber(body[name]) {
			missing = append(missing, name)
		}
	}
	requireBool := func(name string) {
		value := strings.TrimSpace(string(body[name]))
		if value != "true" && value != "false" {
			missing = append(missing, name)
		}
	}
	requireObject := func(name string) {
		value := strings.TrimSpace(string(body[name]))
		if len(value) < 2 || value[0] != '{' || value[len(value)-1] != '}' {
			missing = append(missing, name)
		}
	}
	switch event {
	case hookOnServerStarted:
		requireString("general.mediaServerId", true)
	case hookOnServerKeepalive:
		requireObject("data")
	case hookOnStreamChanged:
		requireString("schema", true)
		requireString("app", true)
		requireString("stream", true)
		requireBool("regist")
	case hookOnStreamNoneReader:
		requireString("schema", true)
		requireString("app", true)
		requireString("stream", true)
	case hookOnRTPServerTimeout:
		requireString("vhost", true)
		requireString("app", true)
		requireString("stream_id", true)
		requireUint("local_port")
		requireUint("tcp_mode")
		requireBool("re_use_port")
		requireUint("ssrc")
	case hookOnPublish:
		requireString("app", true)
		requireString("stream", true)
		requireString("params", false)
		requireString("ip", true)
		requireUint("port")
		requireString("id", true)
	case hookOnPlay:
		requireString("app", true)
		requireString("stream", true)
		requireString("params", false)
		requireString("ip", true)
		requireUint("port")
		requireString("id", true)
	case hookOnFlowReport:
		requireString("schema", true)
		requireString("vhost", true)
		requireString("app", true)
		requireString("stream", true)
		requireBool("player")
		requireUint("totalBytes")
		requireNumber("duration")
	case hookOnStreamNotFound:
		requireString("schema", true)
		requireString("vhost", true)
		requireString("app", true)
		requireString("stream", true)
		requireString("params", false)
	case hookOnRecordMP4:
		requireString("vhost", true)
		requireString("app", true)
		requireString("stream", true)
		requireUint("start_time")
		requireString("file_path", true)
		requireString("file_name", true)
		requireUint("file_size")
		requireNumber("time_len")
	}
	return missing
}

func jsonUnsignedNumber(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.HasPrefix(value, "\"") {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func jsonNumber(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.HasPrefix(value, "\"") {
		return false
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

func (receiver *hookReceiver) recordValidationFailure(event string, fields []string) {
	if len(fields) == 0 {
		fields = []string{"request"}
	}
	copyFields := append([]string(nil), fields...)
	receiver.mu.Lock()
	receiver.validation = append(receiver.validation, hookValidationFailure{event: event, fields: copyFields})
	receiver.mu.Unlock()
}

func (receiver *hookReceiver) clearValidationFailures() {
	receiver.mu.Lock()
	receiver.validation = nil
	receiver.mu.Unlock()
}

func (receiver *hookReceiver) validationFailures() []hookValidationFailure {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	failures := make([]hookValidationFailure, len(receiver.validation))
	copy(failures, receiver.validation)
	return failures
}

func (receiver *hookReceiver) policy(event string, body map[string]json.RawMessage) hookResponse {
	response := hookResponse{Code: 0, Msg: "success"}
	receiver.mu.Lock()
	playToken, publishToken := receiver.playToken, receiver.publishToken
	receiver.mu.Unlock()
	switch event {
	case hookOnPlay:
		if token, present := hookParam(body, "play_token"); !present || token != playToken {
			response.Code = -1
			response.Msg = "fixture playback token rejected"
		}
	case hookOnPublish:
		if token, present := hookParam(body, "publish_token"); !present || token != publishToken {
			response.Code = -1
			response.Msg = "fixture publish token rejected"
		}
	case hookOnStreamNotFound:
		response.Code = -1
		response.Msg = "controlled stream-not-found rejection"
	case hookOnStreamNoneReader:
		response.Close = false
	}
	return response
}

func (receiver *hookReceiver) setTokens(playToken, publishToken string) {
	receiver.mu.Lock()
	receiver.playToken = playToken
	receiver.publishToken = publishToken
	receiver.mu.Unlock()
}

func (receiver *hookReceiver) hookURL(event string) string {
	return receiver.baseURL + "/index/hook/" + event + "?node=" + url.QueryEscape(receiver.node) + "&cap=" + url.QueryEscape(hookCapability(receiver.secret, receiver.node, event))
}

func (receiver *hookReceiver) count(event string) int {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	count := 0
	for _, observation := range receiver.observations {
		if observation.event == event {
			count++
		}
	}
	return count
}

func (receiver *hookReceiver) countCode(event string, responseCode int) int {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	count := 0
	for _, observation := range receiver.observations {
		if observation.event == event && observation.responseCode == responseCode {
			count++
		}
	}
	return count
}

func (receiver *hookReceiver) snapshotCounts() map[string]int {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	counts := make(map[string]int, len(managedHookEvents))
	for _, observation := range receiver.observations {
		counts[observation.event]++
	}
	return counts
}

func (receiver *hookReceiver) snapshotCodeCounts() map[string]map[int]int {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	counts := make(map[string]map[int]int, len(managedHookEvents))
	for _, observation := range receiver.observations {
		if counts[observation.event] == nil {
			counts[observation.event] = make(map[int]int)
		}
		counts[observation.event][observation.responseCode]++
	}
	return counts
}

func (receiver *hookReceiver) waitFor(event string, after int, timeout time.Duration) (hookObservation, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		receiver.mu.Lock()
		seen := 0
		for _, observation := range receiver.observations {
			if observation.event != event {
				continue
			}
			seen++
			if seen > after {
				receiver.mu.Unlock()
				return observation, nil
			}
		}
		receiver.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	return hookObservation{}, fmt.Errorf("hook event %s was not received", event)
}

func (receiver *hookReceiver) waitForCode(event string, responseCode, after int, timeout time.Duration) (hookObservation, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		receiver.mu.Lock()
		seen := 0
		for _, observation := range receiver.observations {
			if observation.event != event || observation.responseCode != responseCode {
				continue
			}
			seen++
			if seen > after {
				receiver.mu.Unlock()
				return observation, nil
			}
		}
		receiver.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	return hookObservation{}, fmt.Errorf("hook event %s with response code %d was not received", event, responseCode)
}

func hookCapability(secret, node, event string) string {
	derivedMAC := hmac.New(sha256.New, []byte(secret))
	_, _ = derivedMAC.Write([]byte("uvp-gb28181/zlm-hook-callback/v2"))
	derived := derivedMAC.Sum(nil)
	capabilityMAC := hmac.New(sha256.New, derived)
	_, _ = capabilityMAC.Write([]byte(node + "\n" + event))
	return base64.RawURLEncoding.EncodeToString(capabilityMAC.Sum(nil))
}

func hookEventFromPath(path string) (string, bool) {
	prefix := "/index/hook/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	event := strings.TrimPrefix(path, prefix)
	for _, managed := range managedHookEvents {
		if event == managed {
			return event, true
		}
	}
	return event, false
}

func readHookBody(request *http.Request) (map[string]json.RawMessage, bool) {
	if request.Body == nil {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, hookRequestBodyLimit+1))
	if err != nil || len(body) == 0 || len(body) > hookRequestBodyLimit {
		return nil, false
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(body, &decoded) != nil || decoded == nil {
		return nil, false
	}
	return decoded, true
}

type hookResponse struct {
	Code  int    `json:"code"`
	Msg   string `json:"msg,omitempty"`
	Close bool   `json:"close"`
}

func writeHookResponse(writer http.ResponseWriter, response hookResponse) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > hookResponseWriteLimit {
		return
	}
	_, _ = writer.Write(encoded)
}

func hookParam(body map[string]json.RawMessage, name string) (string, bool) {
	params, ok := rawString(body["params"])
	if !ok {
		return "", false
	}
	params = strings.TrimPrefix(params, "?")
	values, err := url.ParseQuery(params)
	if err != nil || len(values[name]) != 1 {
		return "", false
	}
	return values[name][0], true
}

func parseJSONUint(raw json.RawMessage) (uint64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.HasPrefix(value, "\"") {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return 0, false
		}
		value = text
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil
}

func checkHookAuthNegative(receiver *hookReceiver) checkResult {
	body := `{"mediaServerId":"` + receiver.node + `","hook_index":1}`
	cases := []struct {
		name  string
		path  string
		query url.Values
	}{
		{name: "wrong_cap", path: "/index/hook/on_play", query: url.Values{"node": {receiver.node}, "cap": {"invalid-capability"}}},
		{name: "wrong_node", path: "/index/hook/on_play", query: url.Values{"node": {"different-node"}, "cap": {hookCapability(receiver.secret, "different-node", hookOnPlay)}}},
		{name: "wrong_event", path: "/index/hook/on_play", query: url.Values{"node": {receiver.node}, "cap": {hookCapability(receiver.secret, receiver.node, hookOnPublish)}}},
	}
	for _, test := range cases {
		status, response, err := postHook(receiver.baseURL+test.path, test.query, []byte(body))
		if err != nil || status != http.StatusOK || response.Code != -1 {
			return failedCheck("hook_auth_negative", "receiver did not reject "+test.name)
		}
	}
	return passedCheck("hook_auth_negative", map[string]any{"wrong_cap": true, "wrong_node": true, "wrong_event": true})
}

func postHook(base string, query url.Values, body []byte) (int, hookResponse, error) {
	requestURL := base
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	request, err := http.NewRequest(http.MethodPost, requestURL, strings.NewReader(string(body)))
	if err != nil {
		return 0, hookResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return 0, hookResponse{}, err
	}
	defer response.Body.Close()
	var decoded hookResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, hookResponseWriteLimit)).Decode(&decoded); err != nil {
		return response.StatusCode, hookResponse{}, err
	}
	return response.StatusCode, decoded, nil
}

func checkHookServerEvents(receiver *hookReceiver) checkResult {
	_, err := receiver.waitFor(hookOnServerStarted, 0, 8*time.Second)
	if err != nil {
		return failedCheck("hook_server_lifecycle", "on_server_started was not received")
	}
	keepaliveBefore := receiver.count(hookOnServerKeepalive)
	_, err = receiver.waitFor(hookOnServerKeepalive, keepaliveBefore, 4*time.Second)
	if err != nil {
		return failedCheck("hook_server_lifecycle", "on_server_keepalive was not received")
	}
	return passedCheck("hook_server_lifecycle", map[string]any{
		"on_server_started":   true,
		"on_server_keepalive": true,
	})
}

func checkHookMediaEvents(receiver *hookReceiver, before map[string]int, beforeCodes map[string]map[int]int) checkResult {
	required := []string{hookOnStreamChanged, hookOnPlay, hookOnFlowReport, hookOnRecordMP4}
	missing := make([]string, 0)
	for _, event := range required {
		if receiver.count(event) <= before[event] {
			if _, err := receiver.waitFor(event, before[event], 5*time.Second); err != nil {
				missing = append(missing, event)
			}
		}
	}
	if len(missing) != 0 {
		return checkResult{Name: "hook_media_events", Status: "failed", Details: map[string]any{"missing": missing}, Error: "real ZLMediaKit media callbacks were not received"}
	}
	if receiver.countCode(hookOnPlay, -1) <= beforeCodes[hookOnPlay][-1] {
		if _, err := receiver.waitForCode(hookOnPlay, -1, beforeCodes[hookOnPlay][-1], 5*time.Second); err != nil {
			return failedCheck("hook_media_events", "on_play did not produce a rejected real callback")
		}
	}
	if receiver.countCode(hookOnPlay, 0) <= beforeCodes[hookOnPlay][0] {
		if _, err := receiver.waitForCode(hookOnPlay, 0, beforeCodes[hookOnPlay][0], 5*time.Second); err != nil {
			return failedCheck("hook_media_events", "on_play did not produce an allowed real callback")
		}
	}
	if receiver.countCode(hookOnPlay, -1) <= beforeCodes[hookOnPlay][-1] || receiver.countCode(hookOnPlay, 0) <= beforeCodes[hookOnPlay][0] {
		return failedCheck("hook_media_events", "on_play did not produce both an allowed and rejected real callback")
	}
	noneReader := "not_executed"
	if receiver.count(hookOnStreamNoneReader) > before[hookOnStreamNoneReader] {
		noneReader = "passed"
	}
	return passedCheck("hook_media_events", map[string]any{
		"on_stream_changed":     true,
		"on_play_allowed":       receiver.countCode(hookOnPlay, 0) > beforeCodes[hookOnPlay][0],
		"on_play_rejected":      receiver.countCode(hookOnPlay, -1) > beforeCodes[hookOnPlay][-1],
		"on_flow_report":        true,
		"on_record_mp4":         true,
		"on_stream_none_reader": noneReader,
	})
}

func checkHookPayloadValidation(receiver *hookReceiver) checkResult {
	failures := receiver.validationFailures()
	if len(failures) == 0 {
		return passedCheck("hook_payload_validation", map[string]any{"validated_callbacks": len(receiver.snapshotCounts())})
	}
	events := make([]string, 0, len(failures))
	fields := make([]string, 0)
	seenEvents := make(map[string]bool)
	seenFields := make(map[string]bool)
	for _, failure := range failures {
		if !seenEvents[failure.event] {
			events = append(events, failure.event)
			seenEvents[failure.event] = true
		}
		for _, field := range failure.fields {
			if !seenFields[field] {
				fields = append(fields, field)
				seenFields[field] = true
			}
		}
	}
	sort.Strings(events)
	sort.Strings(fields)
	return checkResult{
		Name:    "hook_payload_validation",
		Status:  "failed",
		Details: map[string]any{"events": events, "fields": fields},
		Error:   "real Hook callbacks contained missing or incorrectly typed contract fields",
	}
}

func hookEventSummary(receiver *hookReceiver) checkResult {
	counts := receiver.snapshotCounts()
	details := make(map[string]any, len(managedHookEvents))
	for _, event := range managedHookEvents {
		details[event] = counts[event]
	}
	return passedCheck("hook_event_counts", details)
}
