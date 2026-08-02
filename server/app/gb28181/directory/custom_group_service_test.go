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
