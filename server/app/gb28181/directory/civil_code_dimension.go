package directory

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/civilcode"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	// UnassignedID 未分配桶节点 ID
	UnassignedID = "unassigned"

	// 伪节点 ID 前缀(用于层级识别)
	prefixProvince = "province:"
	prefixCity     = "city:"
	prefixDistrict = "district:"
)

// CivilCodeDimension 行政区划维度
//
// 数据源:gb_channel.civil_code group by 三级(省 2位 / 市 4位 / 区县 6位)
// 字典 join:civilcode.Service.Lookup(基于 GB/T 2260,warm cache)
// 未分配桶:civil_code = '000000' 或 空串 或 字典查不到 的通道
//
// 节点 ID 设计(伪节点):
//   - province:<2位>   如 province:37 → 山东省
//   - city:<4位>       如 city:3701   → 济南市
//   - district:<6位>   如 district:370112 → 历城区
//   - unassigned       未分配桶
//   - 通道叶子节点用 gb_channel.channel_id 作为国标编码
type CivilCodeDimension struct {
	db        *gorm.DB
	dictSvc   *civilcode.Service
	scope     ScopeFunc
}

// NewCivilCodeDimension 构造函数
func NewCivilCodeDimension(db *gorm.DB, dictSvc *civilcode.Service, scope ScopeFunc) *CivilCodeDimension {
	if scope == nil {
		scope = func(db *gorm.DB) *gorm.DB { return db }
	}
	return &CivilCodeDimension{db: db, dictSvc: dictSvc, scope: scope}
}

// GetRoots 返回省级伪节点(以及未分配桶,如果有未分配通道)
func (d *CivilCodeDimension) GetRoots(withCounts bool) ([]Node, error) {
	// group by 前 2 位 civil_code(省级)
	type provinceRow struct {
		Province string
		Count    int64
	}
	var rows []provinceRow
	err := d.db.Scopes(d.scope).
		Table("gb_channel").
		Select("SUBSTR(civil_code, 1, 2) AS province, COUNT(*) AS count").
		Where("civil_code IS NOT NULL AND civil_code != '' AND civil_code != '000000' AND LENGTH(civil_code) = 6").
		Group("province").
		Order("province").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("query provinces failed: %w", err)
	}

	nodes := make([]Node, 0, len(rows)+1)
	for _, r := range rows {
		// 尝试查字典拿名字(2 位省级 code 需要补 4 个 0)
		provinceCode6 := r.Province + "0000"
		var displayName string
		if item := d.dictSvc.Lookup(provinceCode6); item != nil {
			displayName = item.Name
		} else {
			displayName = "行政区 " + r.Province
		}

		n := Node{
			ID:          prefixProvince + r.Province,
			Name:        displayName,
			NodeType:    NodeTypeCivilCode,
			CivilCode:   r.Province,
			HasChildren: r.Count > 0,
			IsLeaf:      false,
		}
		if withCounts {
			n.ChannelCount = int(r.Count)
		}
		nodes = append(nodes, n)
	}

	// 未分配桶(civil_code 空 / 000000)
	var unassignedCount int64
	err = d.db.Scopes(d.scope).
		Model(&models.GbChannel{}).
		Where("civil_code IS NULL OR civil_code = '' OR civil_code = '000000'").
		Count(&unassignedCount).Error
	if err != nil {
		return nil, fmt.Errorf("count unassigned failed: %w", err)
	}
	if unassignedCount > 0 {
		n := Node{
			ID:          UnassignedID,
			Name:        "未分配行政区",
			NodeType:    NodeTypeUnassigned,
			HasChildren: true,
			IsLeaf:      false,
		}
		if withCounts {
			n.ChannelCount = int(unassignedCount)
		}
		nodes = append(nodes, n)
	}

	return nodes, nil
}

// GetChildren 按 parentID 分发:
//   - province:XX → 该省的市级列表
//   - city:XXXX → 该市的区县列表
//   - district:XXXXXX → 该区县的通道列表
//   - unassigned → 未分配通道列表
func (d *CivilCodeDimension) GetChildren(parentID string, withCounts bool) ([]Node, error) {
	switch {
	case parentID == UnassignedID:
		return d.getUnassignedChannels(withCounts)
	case strings.HasPrefix(parentID, prefixProvince):
		return d.getCities(strings.TrimPrefix(parentID, prefixProvince), withCounts)
	case strings.HasPrefix(parentID, prefixCity):
		return d.getDistricts(strings.TrimPrefix(parentID, prefixCity), withCounts)
	case strings.HasPrefix(parentID, prefixDistrict):
		return d.getChannelsByCivilCode(strings.TrimPrefix(parentID, prefixDistrict), withCounts)
	default:
		return nil, fmt.Errorf("invalid parentID for civil_code dimension: %s", parentID)
	}
}

// getCities 拿省的市级(前 4 位 group by)
func (d *CivilCodeDimension) getCities(provinceCode string, withCounts bool) ([]Node, error) {
	type row struct {
		City  string
		Count int64
	}
	var rows []row
	err := d.db.Scopes(d.scope).
		Table("gb_channel").
		Select("SUBSTR(civil_code, 1, 4) AS city, COUNT(*) AS count").
		Where("SUBSTR(civil_code, 1, 2) = ? AND civil_code != '000000' AND LENGTH(civil_code) = 6", provinceCode).
		Group("city").
		Order("city").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	provParent := prefixProvince + provinceCode
	nodes := make([]Node, 0, len(rows))
	for _, r := range rows {
		var name string
		city6 := r.City + "00"
		if item := d.dictSvc.Lookup(city6); item != nil {
			name = item.Name
		} else {
			name = "行政区 " + r.City
		}
		n := Node{
			ID:          prefixCity + r.City,
			Name:        name,
			NodeType:    NodeTypeCivilCode,
			CivilCode:   r.City,
			ParentID:    &provParent,
			HasChildren: r.Count > 0,
		}
		if withCounts {
			n.ChannelCount = int(r.Count)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// getDistricts 拿市的区县级(完整 6 位 group by)
func (d *CivilCodeDimension) getDistricts(cityCode string, withCounts bool) ([]Node, error) {
	type row struct {
		District string
		Count    int64
	}
	var rows []row
	err := d.db.Scopes(d.scope).
		Table("gb_channel").
		Select("civil_code AS district, COUNT(*) AS count").
		Where("SUBSTR(civil_code, 1, 4) = ? AND civil_code != '000000' AND LENGTH(civil_code) = 6", cityCode).
		Group("civil_code").
		Order("civil_code").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	cityParent := prefixCity + cityCode
	nodes := make([]Node, 0, len(rows))
	for _, r := range rows {
		var name string
		if item := d.dictSvc.Lookup(r.District); item != nil {
			name = item.Name
		} else {
			name = "行政区 " + r.District
		}
		n := Node{
			ID:          prefixDistrict + r.District,
			Name:        name,
			NodeType:    NodeTypeCivilCode,
			CivilCode:   r.District,
			ParentID:    &cityParent,
			HasChildren: r.Count > 0,
		}
		if withCounts {
			n.ChannelCount = int(r.Count)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// getChannelsByCivilCode 拿指定区县下的通道列表
func (d *CivilCodeDimension) getChannelsByCivilCode(civilCode string, withCounts bool) ([]Node, error) {
	var channels []models.GbChannel
	err := d.db.Scopes(d.scope).
		Where("civil_code = ?", civilCode).
		Order("name, channel_id").
		Find(&channels).Error
	if err != nil {
		return nil, err
	}
	distParent := prefixDistrict + civilCode
	return d.buildChannelNodes(channels, &distParent), nil
}

// getUnassignedChannels 拿未分配通道(civil_code 空 / 000000)
func (d *CivilCodeDimension) getUnassignedChannels(withCounts bool) ([]Node, error) {
	var channels []models.GbChannel
	err := d.db.Scopes(d.scope).
		Where("civil_code IS NULL OR civil_code = '' OR civil_code = '000000'").
		Order("name, channel_id").
		Find(&channels).Error
	if err != nil {
		return nil, err
	}
	un := UnassignedID
	return d.buildChannelNodes(channels, &un), nil
}

// buildChannelNodes 通用 channel → Node 转换
func (d *CivilCodeDimension) buildChannelNodes(channels []models.GbChannel, parent *string) []Node {
	nodes := make([]Node, 0, len(channels))
	for _, ch := range channels {
		chIDStr := strconv.FormatUint(uint64(ch.ID), 10)
		devIDStr := ch.DeviceID
		chCode := ch.ChannelID
		n := Node{
			ID:          "channel-" + chIDStr,
			Name:        ch.Name,
			NodeType:    NodeTypeChannel,
			ParentID:    parent,
			ChannelID:   &chCode,
			DeviceID:    &devIDStr,
			CivilCode:   ch.CivilCode,
			HasChildren: false,
			IsLeaf:      true,
		}
		nodes = append(nodes, n)
	}
	return nodes
}
