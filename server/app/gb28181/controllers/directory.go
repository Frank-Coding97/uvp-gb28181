package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181/civilcode"
	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/global/app"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

type DirectoryController struct {
	controllers.Common
	db        func() *gorm.DB
	syncCache *gbdirectory.SyncCache
}

func NewDirectoryController(db ...func() *gorm.DB) *DirectoryController {
	provider := func() *gorm.DB { return app.DB() }
	if len(db) > 0 && db[0] != nil {
		provider = db[0]
	}
	return &DirectoryController{db: provider, syncCache: gbdirectory.NewSyncCache()}
}

func (dc *DirectoryController) SetDB(provider func() *gorm.DB) { dc.db = provider }

func (dc *DirectoryController) Tree(c *gin.Context) {
	view := c.Query("view")
	if view != "national" && view != "administrative" && view != "department" && view != "business" && view != "custom" {
		dc.Fail(c, "目录视图参数错误", nil, http.StatusBadRequest)
		return
	}
	db := dc.db()
	if db == nil {
		dc.Fail(c, "DB 未就绪", nil, http.StatusServiceUnavailable)
		return
	}
	var tree []gbdirectory.DirectoryNodeVO
	var deptIDs []uint
	var err error
	if view == "department" {
		var buildErr error
		tree, deptIDs, buildErr = buildDepartmentDirectoryTree(c, db)
		if buildErr != nil {
			dc.Fail(c, "生成部门目录失败", buildErr, http.StatusInternalServerError)
			return
		}
	} else if view == "national" || view == "administrative" || view == "business" {
		var scopeErr error
		deptIDs, scopeErr = visibleDirectoryDeptIDs(c, db)
		if scopeErr != nil {
			dc.Fail(c, "读取目录数据范围失败", scopeErr, http.StatusInternalServerError)
			return
		}
		lookup := civilcode.NewService(db)
		if err := lookup.WarmCache(c.Request.Context()); err != nil {
			dc.Fail(c, "加载行政区字典失败", err, http.StatusInternalServerError)
			return
		}
		for _, deptID := range deptIDs {
			var part []gbdirectory.DirectoryNodeVO
			var buildErr error
			switch view {
			case "administrative":
				part, buildErr = gbdirectory.BuildAdministrativeTree(c.Request.Context(), db, deptID, lookup)
			case "business":
				part, buildErr = gbdirectory.BuildBusinessTree(c.Request.Context(), db, deptID)
			default:
				part, buildErr = gbdirectory.BuildNationalTree(c.Request.Context(), db, deptID, lookup)
			}
			if buildErr != nil {
				dc.Fail(c, "生成国标目录失败", buildErr, http.StatusInternalServerError)
				return
			}
			tree = mergeDirectoryNodes(tree, part)
		}
	} else {
		var scopeErr error
		deptIDs, scopeErr = visibleDirectoryDeptIDs(c, db)
		if scopeErr != nil {
			dc.Fail(c, "读取目录数据范围失败", scopeErr, http.StatusInternalServerError)
			return
		}
		deptNames, nameErr := directoryDeptNames(c.Request.Context(), db, deptIDs)
		if nameErr != nil {
			dc.Fail(c, "读取部门名称失败", nameErr, http.StatusInternalServerError)
			return
		}
		for _, deptID := range deptIDs {
			part, buildErr := gbdirectory.BuildCustomTree(c.Request.Context(), db, deptID)
			if buildErr != nil {
				dc.Fail(c, "生成自定义分组失败", buildErr, http.StatusInternalServerError)
				return
			}
			for i := range part {
				if part[i].Type == "ungrouped" {
					part[i].Name = fmt.Sprintf("未分组（%s）", deptNames[deptID])
				}
			}
			tree = append(tree, part...)
		}
	}
	if tree == nil {
		tree = []gbdirectory.DirectoryNodeVO{}
	}
	tree, err = filterDirectoryResources(c, db, tree)
	if err != nil {
		dc.Fail(c, "筛选目录资源失败", err, http.StatusInternalServerError)
		return
	}
	// 在线设备排最上边（规则见 directory.SortOnlineDevicesFirst）。
	// ⛔ 必须放在资源过滤之后、增量快照之前：过滤会砍掉节点，快照要按最终顺序算，
	// 否则前端拿增量时顺序会和上一版对不上，设备上下线后位置不更新。
	gbdirectory.SortOnlineDevicesFirst(tree)
	if err = fillDirectoryDeviceChannelCounts(c.Request.Context(), db, tree); err != nil {
		dc.Fail(c, "统计设备通道数失败", err, http.StatusInternalServerError)
		return
	}
	if c.Query("sync") == "1" {
		scope, _ := json.Marshal([]any{deptIDs, view, c.Query("assetKind"), strings.TrimSpace(c.Query("q")), c.Query("status")})
		dc.Success(c, dc.syncCache.Sync(string(scope), c.Query("since"), tree))
		return
	}
	dc.Success(c, gin.H{"list": tree})
}

func directoryDeptNames(ctx context.Context, db *gorm.DB, ids []uint) (map[uint]string, error) {
	names := make(map[uint]string, len(ids))
	var departments []basemodels.SysDepartment
	if err := db.WithContext(ctx).Select("id", "name").Where("id IN ?", ids).Find(&departments).Error; err != nil {
		return nil, err
	}
	for _, department := range departments {
		names[department.ID] = department.Name
	}
	for _, id := range ids {
		if names[id] == "" {
			if id == 0 {
				names[id] = "默认部门"
			} else {
				names[id] = fmt.Sprintf("部门 %d", id)
			}
		}
	}
	return names, nil
}

func visibleDirectoryDeptIDs(c *gin.Context, db *gorm.DB) ([]uint, error) {
	set := map[uint]struct{}{}
	for _, model := range []any{&gbmodels.GbDevice{}, &gbmodels.GbCustomGroup{}} {
		var ids []uint
		if err := db.WithContext(c.Request.Context()).Model(model).Scopes(ownerDeptScope(c)).Distinct("owner_dept_id").Pluck("owner_dept_id", &ids).Error; err != nil {
			return nil, err
		}
		for _, id := range ids {
			set[id] = struct{}{}
		}
	}
	ids := make([]uint, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func mergeDirectoryNodes(base, incoming []gbdirectory.DirectoryNodeVO) []gbdirectory.DirectoryNodeVO {
	index := make(map[string]int, len(base))
	for i := range base {
		index[base[i].Key] = i
	}
	for _, node := range incoming {
		if i, ok := index[node.Key]; ok {
			base[i].Count += node.Count
			base[i].OnlineCount += node.OnlineCount
			base[i].Children = mergeDirectoryNodes(base[i].Children, node.Children)
			continue
		}
		index[node.Key] = len(base)
		base = append(base, node)
	}
	return base
}
