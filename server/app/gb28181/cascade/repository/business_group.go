package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

type BusinessNodeInput struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ParentCode string `json:"parentCode"`
	Sort       int    `json:"sort"`
}

type BusinessBindingInput struct {
	SourceChannelID uint64 `json:"sourceChannelId"`
	NodeCode        string `json:"nodeCode"`
}

// ReplaceBusinessGroup atomically replaces the business tree and its channel
// assignments. Its revision is shared with channel projections by design.
func (r *GormRepository) ReplaceBusinessGroup(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, nodes []BusinessNodeInput, bindings []BusinessBindingInput) error {
	nodes = append([]BusinessNodeInput(nil), nodes...)
	bindings = append([]BusinessBindingInput(nil), bindings...)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var platform model.GbCascadePlatform
		result := tx.Limit(1).Find(&platform, platformID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrPlatformNotFound
		}
		if platform.ProjectionRevision != expectedProjectionRevision {
			return ErrRevisionConflict
		}
		if platform.PublishGroup && len(nodes) == 0 {
			return invalidBusinessGroup("发布业务分组期间不能清空分组树，请先关闭发布")
		}

		storedNodes, err := validateBusinessGroup(tx, platform, nodes, bindings)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Where("platform_id = ?", platformID).Delete(&model.GbCascadeBusinessBinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("platform_id = ?", platformID).Delete(&model.GbCascadeBusinessNode{}).Error; err != nil {
			return err
		}
		for i := range storedNodes {
			if err := tx.Create(&storedNodes[i]).Error; err != nil {
				return err
			}
		}
		for _, binding := range bindings {
			row := model.GbCascadeBusinessBinding{
				PlatformID: platformID, SourceChannelID: binding.SourceChannelID, NodeCode: binding.NodeCode,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		write := tx.Model(&model.GbCascadePlatform{}).
			Where("id = ? AND projection_revision = ?", platformID, expectedProjectionRevision).
			Updates(map[string]any{
				"projection_revision": gorm.Expr("projection_revision + ?", 1),
				"updated_at":          now,
			})
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected == 0 {
			return ErrRevisionConflict
		}
		return nil
	})
}

func validateBusinessGroup(tx *gorm.DB, platform model.GbCascadePlatform, nodes []BusinessNodeInput, bindings []BusinessBindingInput) ([]model.GbCascadeBusinessNode, error) {
	stored := make([]model.GbCascadeBusinessNode, 0, len(nodes))
	byCode := make(map[string]BusinessNodeInput, len(nodes))
	rootCount := 0
	for _, input := range nodes {
		if len(input.Code) != 20 || !allASCIIDigits(input.Code) {
			return nil, invalidBusinessGroup("节点编号必须是 20 位数字")
		}
		if _, exists := byCode[input.Code]; exists {
			return nil, invalidBusinessGroup("业务分组节点编号重复")
		}
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" {
			return nil, invalidBusinessGroup("节点名称不能为空")
		}
		if utf8.RuneCountInString(input.Name) > 128 {
			return nil, invalidBusinessGroup("节点名称不能超过 128 个字符")
		}
		switch input.Kind {
		case "group":
			rootCount++
			if input.Code[10:13] != "215" || input.ParentCode != "" {
				return nil, invalidBusinessGroup("业务分组根必须使用 215 编码且没有父节点")
			}
		case "organization":
			if input.Code[10:13] != "216" || input.ParentCode == "" {
				return nil, invalidBusinessGroup("虚拟组织必须使用 216 编码并指定父节点")
			}
		default:
			return nil, invalidBusinessGroup("节点类型只能是 group 或 organization")
		}
		byCode[input.Code] = input
		stored = append(stored, model.GbCascadeBusinessNode{
			PlatformID: platform.ID, Code: input.Code, Name: input.Name, Kind: input.Kind,
			ParentCode: input.ParentCode, Sort: input.Sort,
		})
	}
	if len(nodes) > 0 && rootCount != 1 {
		return nil, invalidBusinessGroup("每个平台必须且只能配置一个业务分组根")
	}
	newCodes := make(map[string]struct{}, len(nodes))
	newNodes := make(map[string]BusinessNodeInput, len(nodes))
	for _, input := range nodes {
		newCodes[input.Code] = struct{}{}
		newNodes[input.Code] = input
	}
	var existingNodes []model.GbCascadeBusinessNode
	if err := tx.Where("platform_id = ?", platform.ID).Find(&existingNodes).Error; err != nil {
		return nil, err
	}
	for _, existing := range existingNodes {
		if _, remains := newCodes[existing.Code]; remains {
			continue
		}
		for _, child := range existingNodes {
			if child.ParentCode != existing.Code {
				continue
			}
			newChild, remains := newNodes[child.Code]
			if !remains || newChild.ParentCode == existing.Code {
				return nil, invalidBusinessGroup("不能删除仍有子节点的目录节点，请先在本次保存中移动或删除其内容")
			}
		}
	}
	for _, input := range nodes {
		if input.Kind == "organization" {
			parent, exists := byCode[input.ParentCode]
			if !exists || (parent.Kind != "group" && parent.Kind != "organization") {
				return nil, invalidBusinessGroup("虚拟组织的父节点不存在或类型无效")
			}
		}
	}
	if rootCount == 1 {
		rootCode := ""
		for _, input := range nodes {
			if input.Kind == "group" {
				rootCode = input.Code
				break
			}
		}
		reachable := map[string]bool{rootCode: true}
		remaining := len(nodes) - 1
		for progress := true; progress && remaining > 0; {
			progress = false
			for _, input := range nodes {
				if input.Kind == "organization" && !reachable[input.Code] && reachable[input.ParentCode] {
					reachable[input.Code] = true
					remaining--
					progress = true
				}
			}
		}
		if remaining != 0 {
			return nil, invalidBusinessGroup("业务分组存在孤立节点或循环父子关系")
		}
	}

	if err := validateBusinessCodesAgainstPlatform(tx, platform, nodes); err != nil {
		return nil, err
	}

	bindingChannels := make(map[uint64]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding.SourceChannelID == 0 {
			return nil, invalidBusinessGroup("挂载通道不能为空")
		}
		if _, exists := bindingChannels[binding.SourceChannelID]; exists {
			return nil, invalidBusinessGroup("同一通道在一个上级下只能挂载到一个组织")
		}
		bindingChannels[binding.SourceChannelID] = struct{}{}
		node, exists := byCode[binding.NodeCode]
		if !exists || node.Kind != "organization" {
			return nil, invalidBusinessGroup("通道只能挂载到当前业务树中的虚拟组织")
		}
		var count int64
		if err := tx.Model(&model.GbCascadeChannelProjection{}).
			Where("platform_id = ? AND source_channel_id = ? AND active = ?", platform.ID, binding.SourceChannelID, true).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, invalidBusinessGroup("只能挂载当前上级已共享的有效通道")
		}
	}
	return stored, nil
}

func validateBusinessCodesAgainstPlatform(tx *gorm.DB, platform model.GbCascadePlatform, nodes []BusinessNodeInput) error {
	codes := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Code == platform.LocalDeviceID || node.Code == platform.UpstreamServerID {
			return invalidBusinessGroup("节点编号与平台 SIP 身份编号冲突")
		}
		codes = append(codes, node.Code)
	}
	if len(codes) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&model.GbCascadeDeviceProjection{}).
		Where("platform_id = ? AND active = ? AND published_device_id IN ?", platform.ID, true, codes).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return invalidBusinessGroup("节点编号与已共享设备编号冲突")
	}
	if err := tx.Model(&model.GbCascadeChannelProjection{}).
		Where("platform_id = ? AND active = ? AND published_channel_id IN ?", platform.ID, true, codes).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return invalidBusinessGroup("节点编号与已共享通道编号冲突")
	}
	return nil
}

func allASCIIDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return value != ""
}

func invalidBusinessGroup(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProjection, reason)
}
