package controllers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
)

func (controller *ZLMManagementController) NetworkSessions(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	request, err := parseNetworkSessionQuery(c, nodeID)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Sessions == nil {
		controller.serviceUnavailable(c, nodeID, "network session")
		return
	}
	result, err := controller.bundle.Sessions.ListNetworkSessions(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func parseNetworkSessionQuery(c *gin.Context, nodeID int64) (management.NetworkSessionListRequest, error) {
	page, err := parsePageQuery(c, "localPort", "peerIp")
	if err != nil {
		return management.NetworkSessionListRequest{}, err
	}
	filter := zlm.SessionFilter{}
	if raw, present, queryErr := querySingle(c, "localPort"); queryErr != nil {
		return management.NetworkSessionListRequest{}, queryErr
	} else if present {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value <= 0 || value > 65535 {
			return management.NetworkSessionListRequest{}, management.NewValidationError(map[string]string{"localPort": "must be between 1 and 65535"})
		}
		filter.LocalPort = value
	}
	if raw, present, queryErr := querySingle(c, "peerIp"); queryErr != nil {
		return management.NetworkSessionListRequest{}, queryErr
	} else if present {
		filter.PeerIP = strings.TrimSpace(raw)
	}
	return management.NetworkSessionListRequest{NodeID: nodeID, Filter: filter, Page: page}, nil
}

func (controller *ZLMManagementController) SessionViewers(c *gin.Context) {
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
	if controller == nil || controller.bundle == nil || controller.bundle.Sessions == nil {
		controller.serviceUnavailable(c, nodeID, "media viewer")
		return
	}
	result, err := controller.bundle.Sessions.ListMediaViewers(requestContext(c), management.MediaViewerListRequest{NodeID: nodeID, Media: media, Page: page})
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func (controller *ZLMManagementController) KickSession(c *gin.Context) {
	markManagementAudit(c, "session.kick", 0, nil, "", "", "requested")
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	var request management.KickSessionRequest
	if err := bindManagementJSON(c, &request); err != nil {
		writeManagementError(c, err)
		return
	}
	if err := setNodeID(nodeID, &request.NodeID, "nodeId"); err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Sessions == nil {
		controller.serviceUnavailable(c, nodeID, "session kick")
		return
	}
	target := &management.OwnershipTarget{NodeID: nodeID, Media: request.Media}
	markManagementAudit(c, "session.kick", nodeID, target, "", "", "requested")
	result, err := controller.bundle.Sessions.KickSession(requestContext(c), request)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	markManagementAudit(c, "session.kick", nodeID, target, "", "", managementActionResult(result.Kicked || result.AlreadyDisconnected))
	writeManagementSuccess(c, result)
}
