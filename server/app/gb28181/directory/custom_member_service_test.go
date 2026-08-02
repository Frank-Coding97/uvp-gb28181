package directory

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestCustomMembersAreIdempotentAndScoped(t *testing.T) {
	db := newCustomServiceDB(t)
	groups := NewCustomGroupService(db)
	members := NewCustomMemberService(db)
	a, _ := groups.Create(context.Background(), 10, 1, 0, "A")
	b, _ := groups.Create(context.Background(), 10, 1, 0, "B")
	devices := []gbmodels.GbDevice{{DeviceID: "D1", OwnerDeptID: 10}, {DeviceID: "D2", OwnerDeptID: 10}, {DeviceID: "hidden", OwnerDeptID: 20}}
	require.NoError(t, db.Create(&devices).Error)

	first, err := members.Add(context.Background(), 10, 1, a.ID, []uint{devices[0].ID, devices[1].ID})
	require.NoError(t, err)
	require.Equal(t, 2, first.Added)
	second, err := members.Add(context.Background(), 10, 1, a.ID, []uint{devices[0].ID, devices[1].ID})
	require.NoError(t, err)
	require.Equal(t, 0, second.Added)
	require.Equal(t, 2, second.Skipped)
	_, err = members.Add(context.Background(), 10, 1, a.ID, []uint{devices[0].ID, devices[2].ID})
	require.True(t, errors.Is(err, ErrDeviceNotFound))
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbCustomGroupDevice{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	_, err = members.Add(context.Background(), 10, 1, b.ID, []uint{devices[0].ID})
	require.NoError(t, err)
}

func TestCustomMemberSubtreeAndUngrouped(t *testing.T) {
	db := newCustomServiceDB(t)
	groups := NewCustomGroupService(db)
	members := NewCustomMemberService(db)
	a, _ := groups.Create(context.Background(), 10, 1, 0, "A")
	b, _ := groups.Create(context.Background(), 10, 1, a.ID, "B")
	devices := []gbmodels.GbDevice{{DeviceID: "D1", OwnerDeptID: 10}, {DeviceID: "D2", OwnerDeptID: 10}}
	require.NoError(t, db.Create(&devices).Error)
	_, _ = members.Add(context.Background(), 10, 1, a.ID, []uint{devices[0].ID})
	_, _ = members.Add(context.Background(), 10, 1, b.ID, []uint{devices[0].ID})

	ids, err := members.DeviceIDsForGroup(context.Background(), 10, a.ID)
	require.NoError(t, err)
	require.Equal(t, []uint{devices[0].ID}, ids)
	ungrouped, err := members.UngroupedDeviceIDs(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, []uint{devices[1].ID}, ungrouped)
	require.NoError(t, members.Remove(context.Background(), 10, a.ID, []uint{devices[0].ID}))
	ids, _ = members.DeviceIDsForGroup(context.Background(), 10, b.ID)
	require.Equal(t, []uint{devices[0].ID}, ids)
}

func TestCustomMemberBatchLimit(t *testing.T) {
	db := newCustomServiceDB(t)
	group, _ := NewCustomGroupService(db).Create(context.Background(), 10, 1, 0, "A")
	members := NewCustomMemberService(db)
	_, err := members.Add(context.Background(), 10, 1, group.ID, nil)
	require.True(t, errors.Is(err, ErrDeviceBatchInvalid))
	tooMany := make([]uint, 501)
	_, err = members.Add(context.Background(), 10, 1, group.ID, tooMany)
	require.True(t, errors.Is(err, ErrDeviceBatchInvalid))

	devices := make([]gbmodels.GbDevice, 500)
	for i := range devices {
		devices[i] = gbmodels.GbDevice{DeviceID: fmt.Sprintf("D%03d", i), OwnerDeptID: 10}
	}
	require.NoError(t, db.CreateInBatches(&devices, 100).Error)
	ids := make([]uint, len(devices))
	for i := range devices {
		ids[i] = devices[i].ID
	}
	result, err := members.Add(context.Background(), 10, 1, group.ID, ids)
	require.NoError(t, err)
	require.Equal(t, 500, result.Added)
}
