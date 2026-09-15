package controllers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
)

func (controller *ZLMManagementController) PullProxies(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	page, err := parsePageQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "pull proxy")
		return
	}
	result, err := controller.bundle.Proxies.ListPullProxies(requestContext(c), nodeID, page)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PushProxies(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	page, err := parsePageQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "push proxy")
		return
	}
	result, err := controller.bundle.Proxies.ListPushProxies(requestContext(c), nodeID, page)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) CreatePullProxy(c *gin.Context) {
	markManagementAudit(c, "proxy.create_pull", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.PullProxyRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "pull proxy")
		return
	}
	target := &management.OwnershipTarget{NodeID: nodeID, Media: request.Media}
	markManagementAudit(c, "proxy.create_pull", nodeID, target, "", "", "requested")
	result, err := controller.bundle.Proxies.CreatePullProxy(requestContext(c), uint64(common.GetCurrentUserID(c)), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) CreatePushProxy(c *gin.Context) {
	markManagementAudit(c, "proxy.create_push", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.PushProxyRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "push proxy")
		return
	}
	target := &management.OwnershipTarget{NodeID: nodeID, Media: request.Media}
	markManagementAudit(c, "proxy.create_push", nodeID, target, "", "", "requested")
	result, err := controller.bundle.Proxies.CreatePushProxy(requestContext(c), uint64(common.GetCurrentUserID(c)), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PullProxy(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		writeManagementError(c, management.NewValidationError(map[string]string{"key": "is required"}))
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "pull proxy")
		return
	}
	result, err := controller.bundle.Proxies.GetPullProxy(requestContext(c), nodeID, key)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PushProxy(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		writeManagementError(c, management.NewValidationError(map[string]string{"key": "is required"}))
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "push proxy")
		return
	}
	result, err := controller.bundle.Proxies.GetPushProxy(requestContext(c), nodeID, key)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PreviewDeletePullProxy(c *gin.Context) {
	markManagementAudit(c, "proxy.delete_pull", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.ProxyDeleteRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setProxyPathKey(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "pull proxy")
		return
	}
	result, err := controller.bundle.Proxies.PreviewDeletePullProxy(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PreviewDeletePushProxy(c *gin.Context) {
	markManagementAudit(c, "proxy.delete_push", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.ProxyDeleteRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setProxyPathKey(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "push proxy")
		return
	}
	result, err := controller.bundle.Proxies.PreviewDeletePushProxy(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func setProxyPathKey(c *gin.Context, request *management.ProxyDeleteRequest) error {
	if request == nil {
		return management.NewValidationError(map[string]string{"key": "is required"})
	}
	pathKey := strings.TrimSpace(c.Param("key"))
	if pathKey == "" {
		return management.NewValidationError(map[string]string{"key": "is required"})
	}
	if request.Key == "" {
		request.Key = pathKey
		return nil
	}
	if request.Key != pathKey {
		return management.NewValidationError(map[string]string{"key": "must match the path key"})
	}
	return nil
}

func (controller *ZLMManagementController) DeletePullProxy(c *gin.Context) {
	controller.deleteProxy(c, true)
}

func (controller *ZLMManagementController) DeletePushProxy(c *gin.Context) {
	controller.deleteProxy(c, false)
}

func (controller *ZLMManagementController) deleteProxy(c *gin.Context, pull bool) {
	action := "proxy.delete_push"
	if pull {
		action = "proxy.delete_pull"
	}
	markManagementAudit(c, action, 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.ProxyDeleteRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setProxyPathKey(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Proxies == nil {
		controller.serviceUnavailable(c, nodeID, "proxy delete")
		return
	}
	target := &management.OwnershipTarget{NodeID: nodeID, Media: request.Media}
	markManagementAudit(c, action, nodeID, target, request.Fingerprint, "", "requested")
	var result *management.ProxyDeleteView
	if pull {
		result, err = controller.bundle.Proxies.DeletePullProxy(requestContext(c), uint64(common.GetCurrentUserID(c)), request)
	} else {
		result, err = controller.bundle.Proxies.DeletePushProxy(requestContext(c), uint64(common.GetCurrentUserID(c)), request)
	}
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if result != nil {
		markManagementAudit(c, action, nodeID, target, request.Fingerprint, "", managementActionResult(result.Removed || result.AlreadyAbsent))
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) FFmpegSources(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	page, err := parsePageQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.FFmpeg == nil {
		controller.serviceUnavailable(c, nodeID, "FFmpeg")
		return
	}
	result, err := controller.bundle.FFmpeg.ListSourcesPage(requestContext(c), nodeID, page)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) CreateFFmpegSource(c *gin.Context) {
	markManagementAudit(c, "ffmpeg.create", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.FFmpegSourceCreateRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.FFmpeg == nil {
		controller.serviceUnavailable(c, nodeID, "FFmpeg")
		return
	}
	markManagementAudit(c, "ffmpeg.create", nodeID, &management.OwnershipTarget{NodeID: nodeID}, "", "", "requested")
	result, err := controller.bundle.FFmpeg.CreateSource(requestContext(c), nodeID, uint64(common.GetCurrentUserID(c)), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) DeleteFFmpegSource(c *gin.Context) {
	markManagementAudit(c, "ffmpeg.delete", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		writeManagementError(c, management.NewValidationError(map[string]string{"key": "is required"}))
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.FFmpeg == nil {
		controller.serviceUnavailable(c, nodeID, "FFmpeg")
		return
	}
	result, err := controller.bundle.FFmpeg.Delete(requestContext(c), nodeID, key)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PreflightDeleteFFmpegSource(c *gin.Context) {
	markManagementAudit(c, "ffmpeg.delete", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	key := strings.TrimSpace(c.Param("key"))
	if key == "" {
		writeManagementError(c, management.NewValidationError(map[string]string{"key": "is required"}))
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.FFmpeg == nil {
		controller.serviceUnavailable(c, nodeID, "FFmpeg")
		return
	}
	result, err := controller.bundle.FFmpeg.PreflightDelete(requestContext(c), nodeID, key)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) RTPServers(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	page, err := parsePageQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.RTP == nil {
		controller.serviceUnavailable(c, nodeID, "RTP")
		return
	}
	result, err := controller.bundle.RTP.ListServersPage(requestContext(c), nodeID, page)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) CreateRTPServer(c *gin.Context) {
	markManagementAudit(c, "rtp.create", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RTPServerCreateRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.RTP == nil {
		controller.serviceUnavailable(c, nodeID, "RTP")
		return
	}
	target := &management.OwnershipTarget{NodeID: nodeID, Media: management.MediaIdentity{Schema: "rtsp", Vhost: request.VHost, App: request.App, Stream: request.Stream}}
	markManagementAudit(c, "rtp.create", nodeID, target, "", "", "requested")
	result, err := controller.bundle.RTP.CreateServer(requestContext(c), nodeID, uint64(common.GetCurrentUserID(c)), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) PreflightCloseRTPServer(c *gin.Context) {
	markManagementAudit(c, "rtp.close", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RTPServerCloseRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.RTP == nil {
		controller.serviceUnavailable(c, nodeID, "RTP close")
		return
	}
	result, err := controller.bundle.RTP.PreflightClose(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) CloseRTPServer(c *gin.Context) {
	markManagementAudit(c, "rtp.close", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RTPServerCloseRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.RTP == nil {
		controller.serviceUnavailable(c, nodeID, "RTP close")
		return
	}
	target := &management.OwnershipTarget{NodeID: nodeID, Media: management.MediaIdentity{Schema: "rtsp", Vhost: request.VHost, App: request.App, Stream: request.Stream}}
	markManagementAudit(c, "rtp.close", nodeID, target, "", "", "requested")
	result, err := controller.bundle.RTP.Close(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "rtp.close", nodeID, target, "", "", managementActionResult(result.Released || result.AlreadyReleased))
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) ForceCloseRTPServer(c *gin.Context) {
	markManagementAudit(c, "rtp.force_close", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.RTPServerForceCloseRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.RTP == nil {
		controller.serviceUnavailable(c, nodeID, "RTP force close")
		return
	}
	target := &management.OwnershipTarget{NodeID: nodeID, Media: management.MediaIdentity{Schema: "rtsp", Vhost: request.VHost, App: request.App, Stream: request.Stream}}
	markManagementAudit(c, "rtp.force_close", nodeID, target, "", request.Reason, "requested")
	result, err := controller.bundle.RTP.ForceCloseServer(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "rtp.force_close", nodeID, target, "", request.Reason, managementActionResult(result.Released || result.AlreadyReleased))
	writeManagementSuccess(c, result)
}
