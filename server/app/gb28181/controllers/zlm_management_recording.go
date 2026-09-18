package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
)

func (controller *ZLMManagementController) RecordingStatus(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	request, err := parseRecordingStatusQuery(c, nodeID)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Recording == nil {
		controller.serviceUnavailable(c, nodeID, "recording")
		return
	}
	result, err := controller.bundle.Recording.GetStatus(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func parseRecordingStatusQuery(c *gin.Context, nodeID int64) (management.RecordingStartRequest, error) {
	media, err := parseMediaQuery(c, "type", "recorderType")
	if err != nil {
		return management.RecordingStartRequest{}, err
	}
	request := management.RecordingStartRequest{Target: management.OwnershipTarget{NodeID: nodeID, Media: media}}
	for _, key := range []string{"type", "recorderType"} {
		raw, present, queryErr := querySingle(c, key)
		if queryErr != nil {
			return management.RecordingStartRequest{}, queryErr
		}
		if !present {
			continue
		}
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || (value != int(zlm.RecorderHLS) && value != int(zlm.RecorderMP4)) {
			return management.RecordingStartRequest{}, management.NewValidationError(map[string]string{key: "must be HLS(0) or MP4(1)"})
		}
		if key == "type" {
			request.Type = zlm.RecorderType(value)
		} else {
			request.RecorderType = zlm.RecorderType(value)
		}
	}
	return request, nil
}

func normalizeRecordingStartRequest(nodeID int64, request *management.RecordingStartRequest) error {
	if request == nil {
		return management.NewValidationError(map[string]string{"body": "is required"})
	}
	if request.NodeID != 0 {
		if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
			return err
		}
	}
	if request.Target == (management.OwnershipTarget{}) {
		request.Target = management.OwnershipTarget{NodeID: nodeID, Media: request.Media}
		request.NodeID = nodeID
		return nil
	}
	return setTargetNodeID(nodeID, &request.Target)
}

func normalizeRecordingStopRequest(nodeID int64, request *management.RecordingStopRequest) error {
	if request == nil {
		return management.NewValidationError(map[string]string{"body": "is required"})
	}
	if request.NodeID != 0 {
		if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
			return err
		}
	}
	if request.Target == (management.OwnershipTarget{}) {
		request.Target = management.OwnershipTarget{NodeID: nodeID, Media: request.Media}
		request.NodeID = nodeID
		return nil
	}
	return setTargetNodeID(nodeID, &request.Target)
}

func normalizeRecordingForceStopRequest(nodeID int64, request *management.RecordingForceStopRequest) error {
	if request == nil {
		return management.NewValidationError(map[string]string{"body": "is required"})
	}
	if request.NodeID != 0 {
		if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
			return err
		}
	}
	if request.Target == (management.OwnershipTarget{}) {
		request.Target = management.OwnershipTarget{NodeID: nodeID, Media: request.Media}
		request.NodeID = nodeID
		return nil
	}
	return setTargetNodeID(nodeID, &request.Target)
}

func (controller *ZLMManagementController) PreflightRecording(c *gin.Context) {
	markManagementAudit(c, "recording.start", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RecordingStartRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := normalizeRecordingStartRequest(nodeID, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Recording == nil {
		controller.serviceUnavailable(c, nodeID, "recording")
		return
	}
	result, err := controller.bundle.Recording.Preflight(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) StartRecording(c *gin.Context) {
	markManagementAudit(c, "recording.start", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RecordingStartRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := normalizeRecordingStartRequest(nodeID, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Recording == nil {
		controller.serviceUnavailable(c, nodeID, "recording")
		return
	}
	target := request.Target
	markManagementAudit(c, "recording.start", nodeID, &target, "", "", "requested")
	result, err := controller.bundle.Recording.StartRecording(requestContext(c), uint(common.GetCurrentUserID(c)), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "recording.start", nodeID, &result.Target, "", result.Reason, managementActionResult(result.Recording))
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PreflightStopRecording(c *gin.Context) {
	markManagementAudit(c, "recording.stop", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RecordingStopRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := normalizeRecordingStopRequest(nodeID, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Recording == nil {
		controller.serviceUnavailable(c, nodeID, "recording")
		return
	}
	result, err := controller.bundle.Recording.PreflightStop(requestContext(c), common.GetCurrentUserID(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) StopRecording(c *gin.Context) {
	markManagementAudit(c, "recording.stop", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RecordingStopRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := normalizeRecordingStopRequest(nodeID, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Recording == nil {
		controller.serviceUnavailable(c, nodeID, "recording")
		return
	}
	target := request.Target
	markManagementAudit(c, "recording.stop", nodeID, &target, request.Fingerprint, "", "requested")
	result, err := controller.bundle.Recording.StopRecording(requestContext(c), common.GetCurrentUserID(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "recording.stop", nodeID, &result.Target, request.Fingerprint, result.Reason, managementActionResult(!result.Recording))
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PreflightForceStopRecording(c *gin.Context) {
	markManagementAudit(c, "recording.force_stop", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RecordingForceStopRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := normalizeRecordingForceStopRequest(nodeID, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Recording == nil {
		controller.serviceUnavailable(c, nodeID, "recording")
		return
	}
	result, err := controller.bundle.Recording.PreflightForceStop(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) ForceStopRecording(c *gin.Context) {
	markManagementAudit(c, "recording.force_stop", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RecordingForceStopRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := normalizeRecordingForceStopRequest(nodeID, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Recording == nil {
		controller.serviceUnavailable(c, nodeID, "recording")
		return
	}
	target := request.Target
	markManagementAudit(c, "recording.force_stop", nodeID, &target, request.Fingerprint, request.Reason, "requested")
	result, err := controller.bundle.Recording.ForceStopRecording(requestContext(c), uint(common.GetCurrentUserID(c)), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "recording.force_stop", nodeID, &result.Target, request.Fingerprint, result.Reason, managementActionResult(!result.Recording))
	writeManagementSuccess(c, result)
}
