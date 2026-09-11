package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func checkPublishHook(rtspPort int, receiver *hookReceiver) checkResult {
	deniedBefore := receiver.countCode(hookOnPublish, -1)
	deniedStatus, err := rtspAnnounce(rtspPort, randomToken("publish-denied-"), "invalid-fixture-token")
	if err != nil || deniedStatus != http.StatusUnauthorized {
		return failedCheck("hook_on_publish", "invalid fixture publish token did not produce RTSP 401")
	}
	if _, err := receiver.waitForCode(hookOnPublish, -1, deniedBefore, 5*time.Second); err != nil {
		return failedCheck("hook_on_publish", "the rejected RTSP ANNOUNCE did not produce on_publish")
	}

	allowedBefore := receiver.countCode(hookOnPublish, 0)
	allowedStatus, err := rtspAnnounce(rtspPort, randomToken("publish-allowed-"), receiver.publishToken)
	if err != nil || allowedStatus != http.StatusOK {
		return failedCheck("hook_on_publish", "fixture publish token did not produce RTSP 200")
	}
	if _, err := receiver.waitForCode(hookOnPublish, 0, allowedBefore, 5*time.Second); err != nil {
		return failedCheck("hook_on_publish", "the allowed RTSP ANNOUNCE did not produce on_publish")
	}
	return passedCheck("hook_on_publish", map[string]any{
		"rejected_status": http.StatusUnauthorized,
		"allowed_status":  http.StatusOK,
		"fixture_token":   true,
	})
}

func rtspAnnounce(port int, stream, publishToken string) (int, error) {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	connection, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(6 * time.Second))
	requestURL := url.URL{
		Scheme:   "rtsp",
		Host:     address,
		Path:     "/probe/" + stream,
		RawQuery: url.Values{"publish_token": {publishToken}}.Encode(),
	}
	sdp := strings.Join([]string{
		"v=0",
		"o=- 0 0 IN IP4 127.0.0.1",
		"s=uvp-zlm-probe",
		"t=0 0",
		"m=video 0 RTP/AVP 96",
		"a=rtpmap:96 H264/90000",
		"a=control:trackID=0",
		"",
	}, "\r\n")
	request := fmt.Sprintf("ANNOUNCE %s RTSP/1.0\r\nCSeq: 1\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", requestURL.String(), len([]byte(sdp)), sdp)
	if _, err := io.WriteString(connection, request); err != nil {
		return 0, err
	}
	reader := bufio.NewReaderSize(connection, 4*1024)
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "RTSP/1.0" {
		return 0, fmt.Errorf("unexpected RTSP response")
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("invalid RTSP response status")
	}
	return status, nil
}

func checkRTPTimeout(client *apiClient, secret string, receiver *hookReceiver) checkResult {
	stream := randomToken("rtp-timeout-")
	query := url.Values{
		"port":        {"0"},
		"vhost":       {"__defaultVhost__"},
		"app":         {"rtp"},
		"stream_id":   {stream},
		"tcp_mode":    {"0"},
		"local_ip":    {"127.0.0.1"},
		"re_use_port": {"0"},
		"ssrc":        {strconv.FormatUint(uint64(time.Now().UnixNano())&0xffffffff, 10)},
		"only_track":  {"0"},
		"secret":      {secret},
	}
	opened, err := client.call(context.Background(), "/index/api/openRtpServer", query)
	if err != nil || opened.Code != 0 {
		return failedCheck("hook_on_rtp_server_timeout", "openRtpServer failed for the no-packet timeout check")
	}
	var port int
	if jsonUnmarshalInt(opened.Port, &port) != nil || port <= 0 || port > 65535 {
		return failedCheck("hook_on_rtp_server_timeout", "openRtpServer did not return an allocated timeout-test port")
	}
	closeQuery := url.Values{"vhost": {"__defaultVhost__"}, "app": {"rtp"}, "stream_id": {stream}, "secret": {secret}}
	defer func() { _, _ = client.call(context.Background(), "/index/api/closeRtpServer", closeQuery) }()
	before := receiver.count(hookOnRTPServerTimeout)
	observation, err := receiver.waitFor(hookOnRTPServerTimeout, before, 7*time.Second)
	if err != nil {
		return failedCheck("hook_on_rtp_server_timeout", "the no-packet RTP server did not produce on_rtp_server_timeout")
	}
	if observation.responseCode != 0 {
		return failedCheck("hook_on_rtp_server_timeout", "on_rtp_server_timeout did not receive the success response contract")
	}
	return passedCheck("hook_on_rtp_server_timeout", map[string]any{"timeout_seconds": 2, "allocated_port": port})
}

func checkStreamNotFound(client *apiClient, receiver *hookReceiver) checkResult {
	stream := randomToken("missing-")
	before := receiver.count(hookOnStreamNotFound)
	requestURL := client.baseURL + "/rtp/" + url.PathEscape(stream) + ".live.mp4?" + url.Values{"play_token": {receiver.playToken}}.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return failedCheck("hook_on_stream_not_found", "could not create the missing-stream request")
	}
	response, err := (&http.Client{}).Do(request)
	status := 0
	if err == nil {
		status = response.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8*1024))
		_ = response.Body.Close()
	}
	if _, waitErr := receiver.waitForCode(hookOnStreamNotFound, -1, before, 8*time.Second); waitErr != nil {
		return failedCheck("hook_on_stream_not_found", "missing-stream request did not produce the rejecting Hook")
	}
	if err != nil || status == http.StatusOK {
		return failedCheck("hook_on_stream_not_found", "rejecting on_stream_not_found did not deny the HTTP media request")
	}
	return passedCheck("hook_on_stream_not_found", map[string]any{"http_denied": true, "http_status": status})
}

func jsonUnmarshalInt(raw []byte, target *int) error {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return fmt.Errorf("empty integer")
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return err
	}
	*target = value
	return nil
}
