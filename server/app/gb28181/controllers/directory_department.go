package controllers

import (
	"sort"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	gbdirectory "uvplatform.com/uvp-gb28181/app/gb28181/directory"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/datascope"
)

// visibleDirectoryDepartments reads only departments authorized by the
// current user's existing GB28181 owner-department data scope. Missing parent
// departments remain absent; BuildDepartmentTree detaches those children.
func visibleDirectoryDepartments(c *gin.Context, db *gorm.DB) (basemodels.SysDepartmentList, []uint, error) {
	deptIDs, scoped := datascope.GetOwnerDeptIDsWithDB(c, db)
	departments := basemodels.SysDepartmentList{}
	query := db.WithContext(c.Request.Context()).Model(&basemodels.SysDepartment{})
	if scoped {
		if len(deptIDs) == 0 {
			return departments, []uint{}, nil
		}
		query = query.Where("id IN ?", deptIDs)
	}
	if err := query.Find(&departments).Error; err != nil {
		return nil, nil, err
	}
	if !scoped {
		deptIDs = make([]uint, 0, len(departments))
		for _, department := range departments {
			deptIDs = append(deptIDs, department.ID)
		}
	}
	sort.Slice(deptIDs, func(i, j int) bool { return deptIDs[i] < deptIDs[j] })
	return departments, deptIDs, nil
}

func buildDepartmentDirectoryTree(c *gin.Context, db *gorm.DB) ([]gbdirectory.DirectoryNodeVO, []uint, error) {
	departments, deptIDs, err := visibleDirectoryDepartments(c, db)
	if err != nil {
		return nil, nil, err
	}
	var devices []gbmodels.GbDevice
	if err := db.WithContext(c.Request.Context()).Model(&gbmodels.GbDevice{}).
		Scopes(ownerDeptScope(c)).Order("id").Find(&devices).Error; err != nil {
		return nil, nil, err
	}
	return gbdirectory.BuildDepartmentTree(departments, devices), deptIDs, nil
}
