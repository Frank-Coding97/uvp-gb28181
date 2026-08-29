package controllers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
)

// StreamList serves the stream management page's cross-node list.
func (controller *ZLMManagementController) StreamList(c *gin.Context) {
	request, err := parseStreamListQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, request.Filter.NodeID, "stream")
		return
	}
	result, err := controller.bundle.Streams.ListStreams(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

// NodeStreams is the node-scoped form retained by the API contract. The
// path node is authoritative; a conflicting filter is rejected before the
// service is called.
func (controller *ZLMManagementController) NodeStreams(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	request, err := parseStreamListQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if request.Filter.NodeID != 0 && request.Filter.NodeID != nodeID {
		writeManagementError(c, management.NewValidationError(map[string]string{"nodeId": "must match the path node"}))
		return
	}
	request.Filter.NodeID = nodeID
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, nodeID, "stream")
		return
	}
	result, err := controller.bundle.Streams.ListStreams(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func parseStreamListQuery(c *gin.Context) (management.StreamListRequest, error) {
	allowed := []string{"nodeId", "schema", "vhost", "app", "stream", "originType", "recordingMp4", "recordingHls"}
	page, err := parsePageQuery(c, allowed...)
	if err != nil {
		return management.StreamListRequest{}, err
	}
	values := make(map[string]string, len(allowed))
	for _, key := range allowed {
		raw, present, queryErr := querySingle(c, key)
		if queryErr != nil {
			return management.StreamListRequest{}, queryErr
		}
		if present {
			values[key] = raw
		}
	}
	filter := management.StreamFilter{Schema: values["schema"], Vhost: values["vhost"], App: values["app"], Stream: values["stream"]}
	if values["nodeId"] != "" {
		value, parseErr := parsePositiveTextID(values["nodeId"])
		if parseErr != nil {
			return management.StreamListRequest{}, management.NewValidationError(map[string]string{"nodeId": "must be a positive integer"})
		}
		filter.NodeID = value
	}
	request := management.StreamListRequest{Filter: filter, Page: page}
	if values["originType"] != "" {
		value, parseErr := parseSignedInt(values["originType"])
		if parseErr != nil {
			return management.StreamListRequest{}, management.NewValidationError(map[string]string{"originType": "must be an integer"})
		}
		request.OriginType = &value
	}
	if values["recordingMp4"] != "" {
		value, parseErr := parseBoolText(values["recordingMp4"])
		if parseErr != nil {
			return management.StreamListRequest{}, management.NewValidationError(map[string]string{"recordingMp4": "must be true or false"})
		}
		request.RecordingMP4 = &value
	}
	if values["recordingHls"] != "" {
		value, parseErr := parseBoolText(values["recordingHls"])
		if parseErr != nil {
			return management.StreamListRequest{}, management.NewValidationError(map[string]string{"recordingHls": "must be true or false"})
		}
		request.RecordingHLS = &value
	}
	return request, nil
}

func parsePositiveTextID(value string) (int64, error) {
	result, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || result <= 0 {
		return 0, management.NewValidationError(map[string]string{"nodeId": "must be a positive integer"})
	}
	return result, nil
}

func parseSignedInt(value string) (int, error) {
	result, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, management.NewValidationError(map[string]string{"value": "must be an integer"})
	}
	return result, nil
}

func parseBoolText(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, management.NewValidationError(map[string]string{"value": "must be true or false"})
	}
}

func (controller *ZLMManagementController) StreamDetail(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	media, err := parseMediaQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, nodeID, "stream")
		return
	}
	result, err := controller.bundle.Streams.GetStreamDetail(requestContext(c), nodeID, media)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if result == nil {
		writeManagementError(c, management.NewInternalError(nodeIDText(nodeID), "stream detail is unavailable"))
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) StreamViewers(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	media, page, err := parseMediaPageQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, nodeID, "stream")
		return
	}
	result, err := controller.bundle.Streams.ListStreamViewers(requestContext(c), nodeID, media, page)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PreviewGrant(c *gin.Context) {
	markManagementAudit(c, "stream.preview", 0, nil, "", "", "requested")
	request := management.PreviewGrantRequest{}
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, nodeID, "stream preview")
		return
	}
	markManagementAudit(c, "stream.preview", nodeID, &management.OwnershipTarget{NodeID: nodeID, Media: request.Media}, "", "", "requested")
	result, err := controller.bundle.Streams.IssuePreviewGrant(requestContext(c), uint64(common.GetCurrentUserID(c)), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if result == nil {
		writeManagementError(c, management.NewInternalError(nodeIDText(nodeID), "preview grant is unavailable"))
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) Snapshot(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	media, err := parseMediaQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Snapshot == nil {
		controller.serviceUnavailable(c, nodeID, "snapshot")
		return
	}
	result, err := controller.bundle.Snapshot.FetchSnapshot(requestContext(c), nodeID, media)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if len(result.Body) == 0 || result.ContentType != "image/jpeg" {
		writeManagementError(c, management.NewInternalError(nodeIDText(nodeID), "snapshot response is invalid"))
		return
	}
	// This marker is deliberately the last operation before headers/body are
	// written. It prevents operation-log response capture even if the
	// middleware wraps the Gin writer.
	target := &management.OwnershipTarget{NodeID: nodeID, Media: media}
	markManagementAudit(c, "stream.snapshot", nodeID, target, "", "", "jpeg")
	c.Header("Cache-Control", "no-store")
	c.Data(200, "image/jpeg", result.Body)
}

func (controller *ZLMManagementController) PreflightCloseStream(c *gin.Context) {
	markManagementAudit(c, "stream.close", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var target management.OwnershipTarget
	if err := bindManagementJSON(c, &target); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setTargetNodeID(nodeID, &target); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, nodeID, "stream close")
		return
	}
	result, err := controller.bundle.Streams.PreflightCloseStream(requestContext(c), target)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) CloseStream(c *gin.Context) {
	markManagementAudit(c, "stream.close", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.CloseStreamRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setTargetNodeID(nodeID, &request.Target); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, nodeID, "stream close")
		return
	}
	markManagementAudit(c, "stream.close", nodeID, &request.Target, request.Fingerprint, "", "requested")
	result, err := controller.bundle.Streams.CloseStream(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "stream.close", nodeID, &result.Target, request.Fingerprint, "", managementActionResult(result.Closed || result.AlreadyAbsent))
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) ForceCloseStream(c *gin.Context) {
	markManagementAudit(c, "stream.force_close", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.ForceCloseStreamRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setTargetNodeID(nodeID, &request.Target); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, nodeID, "stream force close")
		return
	}
	target := request.Target
	markManagementAudit(c, "stream.force_close", nodeID, &target, request.Fingerprint, request.Reason, "requested")
	result, err := controller.bundle.Streams.ForceCloseStream(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "stream.force_close", nodeID, &result.Target, request.Fingerprint, request.Reason, managementActionResult(result.Closed || result.AlreadyAbsent))
	writeManagementSuccess(c, result)
}

type streamBatchCloseRequest struct {
	Targets     []management.OwnershipTarget   `json:"targets"`
	Snapshots   []management.OwnershipSnapshot `json:"snapshots"`
	Fingerprint string                         `json:"fingerprint"`
}

func (controller *ZLMManagementController) PreflightCloseStreams(c *gin.Context) {
	markManagementAudit(c, "stream.batch_close", 0, nil, "", "", "requested")
	var request struct {
		Targets []management.OwnershipTarget `json:"targets"`
	}
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, 0, "stream batch close")
		return
	}
	result, err := controller.bundle.Streams.PreflightCloseStreams(requestContext(c), request.Targets)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) CloseStreams(c *gin.Context) {
	markManagementAudit(c, "stream.batch_close", 0, nil, "", "", "requested")
	var request streamBatchCloseRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Streams == nil {
		controller.serviceUnavailable(c, 0, "stream batch close")
		return
	}
	result, err := controller.bundle.Streams.CloseStreams(requestContext(c), management.OwnershipBatchPreflight{
		Targets: request.Targets, Snapshots: request.Snapshots, Fingerprint: request.Fingerprint,
	})
	if err != nil {
		writeManagementError(c, err)
		return
	}
	outcomes := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		switch {
		case item.Closed:
			outcomes = append(outcomes, "closed")
		case item.AlreadyAbsent:
			outcomes = append(outcomes, "already_absent")
		case item.Uncertain:
			outcomes = append(outcomes, "uncertain")
		default:
			outcomes = append(outcomes, "failed")
		}
	}
	markManagementAudit(c, "stream.batch_close", 0, nil, request.Fingerprint, "", boundedBatchActionResult(outcomes))
	writeManagementSuccess(c, result)
}
