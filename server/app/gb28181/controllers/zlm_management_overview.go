package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
)

// Overview serves the cross-node dashboard. It deliberately uses the
// management service's bounded aggregation result, including partial/error
// evidence, rather than exposing any ZLM response shape.
func (controller *ZLMManagementController) Overview(c *gin.Context) {
	if controller == nil || controller.bundle == nil || controller.bundle.Overview == nil {
		controller.serviceUnavailable(c, 0, "overview")
		return
	}
	result, err := controller.bundle.Overview.GetOverview(requestContext(c))
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

// Runtime serves one node's live runtime summary.
func (controller *ZLMManagementController) Runtime(c *gin.Context) {
	nodeID, err := parsePositivePathID(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Overview == nil {
		controller.serviceUnavailable(c, nodeID, "runtime")
		return
	}
	result, err := controller.bundle.Overview.GetNodeRuntime(requestContext(c), nodeID)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

// Streams aggregates runtime streams across visible nodes. A nodeId query
// restricts the typed service filter but never changes the route family.
func (controller *ZLMManagementController) Streams(c *gin.Context) {
	filter, page, err := parseOverviewStreamQuery(c)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	if controller == nil || controller.bundle == nil || controller.bundle.Overview == nil {
		controller.serviceUnavailable(c, filter.NodeID, "stream overview")
		return
	}
	result, err := controller.bundle.Overview.ListStreams(requestContext(c), filter, page)
	if err != nil {
		writeManagementError(c, err)
		return
	}
	writeManagementSuccess(c, result)
}

func parseOverviewStreamQuery(c *gin.Context) (management.StreamFilter, management.PageRequest, error) {
	allowed := []string{"nodeId", "schema", "vhost", "app", "stream"}
	page, err := parsePageQuery(c, allowed...)
	if err != nil {
		return management.StreamFilter{}, management.PageRequest{}, err
	}
	values := make(map[string]string, len(allowed))
	for _, key := range allowed {
		raw, present, queryErr := querySingle(c, key)
		if queryErr != nil {
			return management.StreamFilter{}, management.PageRequest{}, queryErr
		}
		if present {
			values[key] = raw
		}
	}
	filter := management.StreamFilter{Schema: values["schema"], Vhost: values["vhost"], App: values["app"], Stream: values["stream"]}
	if raw, present, queryErr := querySingle(c, "nodeId"); queryErr != nil {
		return management.StreamFilter{}, management.PageRequest{}, queryErr
	} else if present {
		nodeID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || nodeID <= 0 {
			return management.StreamFilter{}, management.PageRequest{}, management.NewValidationError(map[string]string{"nodeId": "must be a positive integer"})
		}
		filter.NodeID = nodeID
	}
	return filter, page, nil
}
