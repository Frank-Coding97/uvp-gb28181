package controllers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbcatalog "uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestAnomaly_ChangeMountRejectsOtherOwnerDept(t *testing.T) {
	const userID uint = 100
	r, db := newDeviceMgmtRouter(t, withClaims(userID))
	seedDeptScopedUser(t, db, userID, 10)

	node := &gbmodels.GbCatalogNode{
		OwnerDeptID: 10,
		NodeType:    gbmodels.NodeTypeVirtualOrg,
		Path:        "/1/2/",
		Name:        "异常节点",
		Anomaly:     true,
	}
	target := &gbmodels.GbCatalogNode{
		OwnerDeptID: 20,
		NodeType:    gbmodels.NodeTypeCivilCode,
		Path:        "/9/",
		Name:        "外部门节点",
	}
	require.NoError(t, db.Create(node).Error)
	require.NoError(t, db.Create(target).Error)
	rec := &gbmodels.GbAnomalyRecord{
		OwnerDeptID:   10,
		CatalogNodeID: node.ID,
		RawCode:       "XYZ",
		FallbackType:  gbmodels.FallbackTypeVirtualOrg,
		Resolved:      false,
	}
	require.NoError(t, db.Create(rec).Error)

	body, _ := json.Marshal(map[string]any{
		"action":         "change-mount",
		"targetParentId": target.ID,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(
		http.MethodPost,
		"/api/gb28181/device-mgmt/anomaly/"+uintStr(rec.ID)+"/resolve",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	resp := unmarshal(t, w)
	assert.EqualValues(t, 1, resp["code"])
	assert.Equal(t, "resolve 失败", resp["message"])

	var unchangedRecord gbmodels.GbAnomalyRecord
	require.NoError(t, db.First(&unchangedRecord, rec.ID).Error)
	assert.False(t, unchangedRecord.Resolved)

	var unchangedNode gbmodels.GbCatalogNode
	require.NoError(t, db.First(&unchangedNode, node.ID).Error)
	assert.Nil(t, unchangedNode.ParentID)
	assert.Equal(t, "/1/2/", unchangedNode.Path)
	assert.True(t, unchangedNode.Anomaly)
}

func TestAnomaly_ChangeMountRebuildsSubtreePath(t *testing.T) {
	const userID uint = 100
	r, db := newDeviceMgmtRouter(t, withClaims(userID))
	seedDeptScopedUser(t, db, userID, 10)

	node := &gbmodels.GbCatalogNode{
		OwnerDeptID: 10,
		NodeType:    gbmodels.NodeTypeVirtualOrg,
		Path:        "/1/2/",
		Name:        "异常节点",
		Anomaly:     true,
	}
	target := &gbmodels.GbCatalogNode{
		OwnerDeptID: 10,
		NodeType:    gbmodels.NodeTypeCivilCode,
		Path:        "/1/9/",
		Name:        "新父节点",
	}
	require.NoError(t, db.Create(node).Error)
	require.NoError(t, db.Create(target).Error)
	child := &gbmodels.GbCatalogNode{
		OwnerDeptID: 10,
		NodeType:    gbmodels.NodeTypeChannel,
		ParentID:    &node.ID,
		Path:        "/",
		Name:        "子节点",
	}
	require.NoError(t, db.Create(child).Error)
	child.Path = gbcatalog.BuildPath(node.Path, child.ID)
	child.Depth = gbcatalog.DepthFromPath(child.Path)
	require.NoError(t, db.Model(child).Updates(map[string]any{
		"path":  child.Path,
		"depth": child.Depth,
	}).Error)
	rec := &gbmodels.GbAnomalyRecord{
		OwnerDeptID:   10,
		CatalogNodeID: node.ID,
		RawCode:       "XYZ",
		FallbackType:  gbmodels.FallbackTypeVirtualOrg,
		Resolved:      false,
	}
	require.NoError(t, db.Create(rec).Error)

	body, _ := json.Marshal(map[string]any{
		"action":         "change-mount",
		"targetParentId": target.ID,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(
		http.MethodPost,
		"/api/gb28181/device-mgmt/anomaly/"+uintStr(rec.ID)+"/resolve",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	assert.EqualValues(t, 0, resp["code"])

	var updatedNode, updatedChild gbmodels.GbCatalogNode
	require.NoError(t, db.First(&updatedNode, node.ID).Error)
	require.NoError(t, db.First(&updatedChild, child.ID).Error)
	expectedNodePath := gbcatalog.BuildPath(target.Path, node.ID)
	assert.Equal(t, expectedNodePath, updatedNode.Path)
	assert.Equal(t, gbcatalog.DepthFromPath(expectedNodePath), updatedNode.Depth)
	assert.Equal(t, gbcatalog.BuildPath(expectedNodePath, child.ID), updatedChild.Path)
	assert.Equal(t, gbcatalog.DepthFromPath(updatedChild.Path), updatedChild.Depth)
}
