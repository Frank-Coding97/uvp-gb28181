package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newChunkScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open("file:chunk-scope?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SysAffixChunk{}, &SysAffix{}))
	return db
}

// 续传会话必须按 created_by 隔离：
// 用户 22 拿着用户 11 的 uploadId，不能查到任何分片，也不能合并。
func TestGetChunksByUploadIdIsolatesUsers(t *testing.T) {
	db := newChunkScopeDB(t)

	const uploadID = "upload_1700000000_deadbeef"
	for _, index := range []int{0, 1} {
		require.NoError(t, db.Create(&SysAffixChunk{
			UploadId:    uploadID,
			FileMd5:     "d41d8cd98f00b204e9800998ecf8427e",
			TotalChunks: 2,
			ChunkIndex:  index,
			Status:      0,
			CreatedBy:   11,
		}).Error)
	}

	ownerChunks, err := getChunksByUploadId(db, uploadID, 11)
	require.NoError(t, err)
	require.Len(t, *ownerChunks, 2)

	otherChunks, err := getChunksByUploadId(db, uploadID, 22)
	require.NoError(t, err)
	require.Empty(t, *otherChunks, "别的用户必须查不到任何分片")

	ownerID, found, err := getChunkOwnerID(db, uploadID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint(11), ownerID)

	_, found, err = getChunkOwnerID(db, "upload_1700000000_0badf00d")
	require.NoError(t, err)
	require.False(t, found, "没有分片记录时应返回 found=false")
}

// 秒传只能命中自己的文件 —— 否则知道别人文件的 MD5 就能拿到它的 path/url。
func TestGetAffixByMd5IsolatesUsers(t *testing.T) {
	db := newChunkScopeDB(t)

	const md5 = "5d41402abc4b2a76b9719d911017c592"
	require.NoError(t, db.Create(&SysAffix{
		Name:      "私有文件.txt",
		Path:      "/srv/uploads/private.txt",
		Url:       "/public/uploads/private.txt",
		Size:      11,
		FileMd5:   md5,
		CreatedBy: 11,
	}).Error)

	found, err := getAffixByMd5(db, md5, 11, 11)
	require.NoError(t, err)
	require.Equal(t, uint(11), found.CreatedBy)

	_, err = getAffixByMd5(db, md5, 11, 22)
	require.Error(t, err, "别的用户不能秒传命中，更不能拿到对方的存储地址")

	// 同用户但大小不同也不算命中（保持原有语义）
	_, err = getAffixByMd5(db, md5, 12, 11)
	require.Error(t, err)
}
