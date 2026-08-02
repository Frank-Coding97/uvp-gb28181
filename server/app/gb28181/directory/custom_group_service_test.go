package directory

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newCustomServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newPlacementDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}))
	return db
}

func TestCustomGroupCreateAndRename(t *testing.T) {
	db := newCustomServiceDB(t)
	svc := NewCustomGroupService(db)
	root, err := svc.Create(context.Background(), 10, 7, 0, " 东区停车场 ")
	require.NoError(t, err)
	require.Equal(t, "东区停车场", root.Name)
	require.Equal(t, 10, int(root.OwnerDeptID))
	require.Equal(t, "/"+strconv.Itoa(int(root.ID))+"/", root.Path)

	child, err := svc.Create(context.Background(), 10, 7, root.ID, "入口")
	require.NoError(t, err)
	require.Equal(t, root.Path+strconv.Itoa(int(child.ID))+"/", child.Path)
	require.Equal(t, uint8(1), child.Depth)

	_, err = svc.Create(context.Background(), 10, 7, root.ID, "入口")
	require.True(t, errors.Is(err, ErrGroupNameConflict))
	require.NoError(t, svc.Rename(context.Background(), 10, child.ID, "入口"))
	require.NoError(t, svc.Rename(context.Background(), 10, child.ID, "南入口"))
}

func TestCustomGroupCreateRejectsInvalidAndHiddenParent(t *testing.T) {
	db := newCustomServiceDB(t)
	svc := NewCustomGroupService(db)
	_, err := svc.Create(context.Background(), 10, 1, 0, "   ")
	require.True(t, errors.Is(err, ErrGroupNameInvalid))
	_, err = svc.Create(context.Background(), 10, 1, 0, strings.Repeat("组", 65))
	require.True(t, errors.Is(err, ErrGroupNameInvalid))
	hidden, err := svc.Create(context.Background(), 20, 1, 0, "外部门")
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), 10, 1, hidden.ID, "子组")
	require.True(t, errors.Is(err, ErrGroupNotFound))
}

func TestCustomGroupMoveUpdatesSubtreeAndRejectsCycle(t *testing.T) {
	db := newCustomServiceDB(t)
	svc := NewCustomGroupService(db)
	a, _ := svc.Create(context.Background(), 10, 1, 0, "A")
	b, _ := svc.Create(context.Background(), 10, 1, a.ID, "B")
	d, _ := svc.Create(context.Background(), 10, 1, b.ID, "D")
	c, _ := svc.Create(context.Background(), 10, 1, 0, "C")

	require.NoError(t, svc.Move(context.Background(), 10, b.ID, c.ID))
	updatedB, _ := findGroup(db, 10, b.ID)
	updatedD, _ := findGroup(db, 10, d.ID)
	require.Equal(t, c.Path+strconv.Itoa(int(b.ID))+"/", updatedB.Path)
	require.Equal(t, updatedB.Path+strconv.Itoa(int(d.ID))+"/", updatedD.Path)
	require.Equal(t, uint8(1), updatedB.Depth)
	require.Equal(t, uint8(2), updatedD.Depth)

	require.True(t, errors.Is(svc.Move(context.Background(), 10, b.ID, b.ID), ErrGroupCycle))
	require.True(t, errors.Is(svc.Move(context.Background(), 10, c.ID, d.ID), ErrGroupCycle))
	hidden, _ := svc.Create(context.Background(), 20, 1, 0, "hidden")
	require.True(t, errors.Is(svc.Move(context.Background(), 10, b.ID, hidden.ID), ErrGroupNotFound))
}

func TestCustomGroupDeleteIsSafe(t *testing.T) {
	db := newCustomServiceDB(t)
	svc := NewCustomGroupService(db)
	parent, _ := svc.Create(context.Background(), 10, 1, 0, "parent")
	child, _ := svc.Create(context.Background(), 10, 1, parent.ID, "child")
	_, err := svc.Delete(context.Background(), 10, parent.ID)
	require.True(t, errors.Is(err, ErrGroupHasChildren))
	var domain *DomainError
	require.True(t, errors.As(err, &domain))
	require.Equal(t, 1, domain.Details["childCount"])

	devices := []gbmodels.GbDevice{{DeviceID: "D1", OwnerDeptID: 10}, {DeviceID: "D2", OwnerDeptID: 10}}
	require.NoError(t, db.Create(&devices).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbCustomGroupDevice{{GroupID: child.ID, DeviceID: devices[0].ID}, {GroupID: child.ID, DeviceID: devices[1].ID}}).Error)
	result, err := svc.Delete(context.Background(), 10, child.ID)
	require.NoError(t, err)
	require.Equal(t, 2, result.RemovedDeviceCount)
	var deviceCount int64
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Count(&deviceCount).Error)
	require.EqualValues(t, 2, deviceCount)
}
