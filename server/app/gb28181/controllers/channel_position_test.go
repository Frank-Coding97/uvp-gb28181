package controllers_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// 人工录入通道坐标（三通路里的 manual 这一路）。
//
// 覆盖的判据（每一条都对应一个会被下一个人踩的坑）：
//   - 成对校验：只给经度或只给纬度必须被拒，库里不能出现"半对坐标"；
//   - 值域校验：越界直接拒，不落库；
//   - 0 的语义：两者同为 0 = 清除；只给其一为 0 = 非法；
//   - 来源标记：写入后 position_source 必须是 manual（人工值不被目录刷新覆盖靠它）；
//   - 只读不写：校验失败的请求**不得**改到该行任何字段。

func patchChannelPosition(t *testing.T, r http.Handler, id uint, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PATCH", "/api/gb28181/device-mgmt/channel/"+uintStr(id), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// loadChannel 每次都读进**新建**结构体。
//
// ⛔ 不能复用一个变量重复 First：GORM 往一个已填过的结构体里扫描时，
// 库里为 NULL 的可空列**不会**把原有的指针置回 nil（实测 `*time.Time` 如此），
// 于是"清除成功了没有"这类断言会拿到上一轮的值。测试侧一律这样读。
func loadChannel(t *testing.T, db *gorm.DB, id uint) gbmodels.GbChannel {
	t.Helper()
	var ch gbmodels.GbChannel
	require.NoError(t, db.First(&ch, id).Error)
	return ch
}

func TestDeviceMgmt_UpdateChannelManualPosition(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	w := patchChannelPosition(t, r, chOnID, `{"longitude":117.123456,"latitude":36.654321}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	ch := loadChannel(t, db, chOnID)
	assert.InDelta(t, 117.123456, ch.Longitude, 1e-6)
	assert.InDelta(t, 36.654321, ch.Latitude, 1e-6)
	assert.Equal(t, gbmodels.ChannelPositionSourceManual, ch.PositionSource)
	require.NotNil(t, ch.PositionUpdatedAt, "人工录入必须同时盖时间戳")
}

// 只给一个分量 = 非法。静默接受会让库里出现"经度是新值、纬度还是旧值"的半对状态。
func TestDeviceMgmt_UpdateChannelPositionRejectsHalfPair(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	for _, body := range []string{`{"longitude":117.5}`, `{"latitude":36.5}`} {
		w := patchChannelPosition(t, r, chOnID, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, body+": "+w.Body.String())
	}

	// 种子值原样保留（36.685 / 117.05），来源也没被写上。
	ch := loadChannel(t, db, chOnID)
	assert.InDelta(t, 117.05, ch.Longitude, 1e-6)
	assert.InDelta(t, 36.685, ch.Latitude, 1e-6)
	assert.Empty(t, ch.PositionSource)
}

// 只给其一为 0 = 非法（"半对"的另一种形态）；两者同为 0 = 清除。
func TestDeviceMgmt_UpdateChannelPositionZeroSemantics(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	w := patchChannelPosition(t, r, chOnID, `{"longitude":0,"latitude":36.5}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	w = patchChannelPosition(t, r, chOnID, `{"longitude":117.5,"latitude":0}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	assert.InDelta(t, 117.05, loadChannel(t, db, chOnID).Longitude, 1e-6, "被拒的请求不能改到该行")

	// 先写一个合法的人工坐标，再用 0/0 清除。
	w = patchChannelPosition(t, r, chOnID, `{"longitude":117.5,"latitude":36.5}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, gbmodels.ChannelPositionSourceManual, loadChannel(t, db, chOnID).PositionSource)

	w = patchChannelPosition(t, r, chOnID, `{"longitude":0,"latitude":0}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ch := loadChannel(t, db, chOnID)
	assert.Zero(t, ch.Longitude)
	assert.Zero(t, ch.Latitude)
	assert.Empty(t, ch.PositionSource, "清除后必须与「从来没配过」同形")
	assert.Nil(t, ch.PositionUpdatedAt)
}

func TestDeviceMgmt_UpdateChannelPositionRejectsOutOfRange(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	for _, body := range []string{
		`{"longitude":180.000001,"latitude":36.5}`,
		`{"longitude":-180.000001,"latitude":36.5}`,
		`{"longitude":117.5,"latitude":90.000001}`,
		`{"longitude":117.5,"latitude":-90.000001}`,
	} {
		w := patchChannelPosition(t, r, chOnID, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, body+": "+w.Body.String())
	}

	// 边界值本身合法。
	w := patchChannelPosition(t, r, chOnID, `{"longitude":180,"latitude":90}`)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// 列表接口必须把来源列带出去 —— 前端靠它说明"这个坐标是谁写的"。
// 少了这个字段，界面只能显示一个没有出处的坐标。
func TestDeviceMgmt_ListChannelsCarriesPositionSource(t *testing.T) {
	r, db := newDeviceMgmtRouter(t)
	_, chOnID, _ := seedDevicesAndChannels(t, db)

	w := patchChannelPosition(t, r, chOnID, `{"longitude":117.5,"latitude":36.5}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/channels", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	data := unmarshal(t, w)["data"].(map[string]any)
	list := data["list"].([]any)
	var found bool
	for _, raw := range list {
		item := raw.(map[string]any)
		if item["channelId"] != "37011200001310000001" {
			continue
		}
		found = true
		assert.Equal(t, gbmodels.ChannelPositionSourceManual, item["positionSource"])
		assert.InDelta(t, 117.5, item["longitude"], 1e-6)
		// 时间戳非空 —— 没有它无法判断"实时坐标是不是已经陈旧"。
		assert.NotEmpty(t, item["positionUpdatedAt"])
	}
	require.True(t, found, "列表里必须能找到该通道")
}
