package controllers

import (
	"context"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	gbdirectory "uvplatform.cn/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type DirectoryController struct {
	controllers.Common
	db func() *gorm.DB
}

func NewDirectoryController(db ...func() *gorm.DB) *DirectoryController {
	provider := func() *gorm.DB { return app.DB() }
	if len(db) > 0 && db[0] != nil {
		provider = db[0]
	}
	return &DirectoryController{db: provider}
}

func (dc *DirectoryController) SetDB(provider func() *gorm.DB) { dc.db = provider }

func (dc *DirectoryController) Tree(c *gin.Context) {
	view := c.Query("view")
	if view != "national" && view != "custom" {
		dc.Fail(c, "目录视图参数错误", nil, http.StatusBadRequest)
		return
	}
	db := dc.db()
	if db == nil {
		dc.Fail(c, "DB 未就绪", nil, http.StatusServiceUnavailable)
		return
	}
	deptIDs, err := visibleDirectoryDeptIDs(c, db)
	if err != nil {
		dc.Fail(c, "读取目录数据范围失败", err, http.StatusInternalServerError)
		return
	}
	var tree []gbdirectory.DirectoryNodeVO
	if view == "national" {
		lookup := civilcode.NewService(db)
		if err := lookup.WarmCache(c.Request.Context()); err != nil {
			dc.Fail(c, "加载行政区字典失败", err, http.StatusInternalServerError)
			return
		}
		for _, deptID := range deptIDs {
			part, buildErr := gbdirectory.BuildNationalTree(c.Request.Context(), db, deptID, lookup)
			if buildErr != nil {
				dc.Fail(c, "生成国标目录失败", buildErr, http.StatusInternalServerError)
				return
			}
			tree = mergeDirectoryNodes(tree, part)
		}
	} else {
		for _, deptID := range deptIDs {
			part, buildErr := gbdirectory.BuildCustomTree(c.Request.Context(), db, deptID)
			if buildErr != nil {
				dc.Fail(c, "生成自定义分组失败", buildErr, http.StatusInternalServerError)
				return
			}
			tree = append(tree, part...)
		}
	}
	if tree == nil {
		tree = []gbdirectory.DirectoryNodeVO{}
	}
	dc.Success(c, gin.H{"list": tree})
}

func visibleDirectoryDeptIDs(c *gin.Context, db *gorm.DB) ([]uint, error) {
	set := map[uint]struct{}{}
	for _, model := range []any{&gbmodels.GbDevice{}, &gbmodels.GbCustomGroup{}} {
		var ids []uint
		if err := db.WithContext(context.Background()).Model(model).Scopes(ownerDeptScope(c)).Distinct("owner_dept_id").Pluck("owner_dept_id", &ids).Error; err != nil {
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
