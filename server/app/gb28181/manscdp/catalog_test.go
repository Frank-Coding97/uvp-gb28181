package manscdp

import (
	"strings"
	"testing"
)

// TestBuildCatalogQuery T3-测1: Catalog 查询 XML 构造符合国标
func TestBuildCatalogQuery(t *testing.T) {
	body, err := BuildCatalogQuery("34020000001320000018", 1)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "<CmdType>Catalog</CmdType>") {
		t.Error("缺 CmdType")
	}
	if !strings.Contains(s, "<DeviceID>34020000001320000018</DeviceID>") {
		t.Error("缺 DeviceID")
	}
	if !strings.Contains(s, "<SN>1</SN>") {
		t.Error("缺 SN")
	}
	if !strings.Contains(s, "<Query>") {
		t.Error("根元素应为 Query")
	}
}

// TestParseCatalogResponse T3-测2: 解析 Catalog 应答,提取通道项
func TestParseCatalogResponse(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Response>
<CmdType>Catalog</CmdType>
<SN>1</SN>
<DeviceID>34020000001320000018</DeviceID>
<SumNum>2</SumNum>
<DeviceList Num="2">
<Item>
<DeviceID>34020000001320000019</DeviceID>
<Name>Camera1</Name>
<Manufacturer>Hikvision</Manufacturer>
<Status>ON</Status>
<PTZType>1</PTZType>
</Item>
<Item>
<DeviceID>34020000001320000020</DeviceID>
<Name>Camera2</Name>
<Status>OFF</Status>
</Item>
</DeviceList>
</Response>`)
	resp, err := ParseCatalogResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.SumNum != 2 {
		t.Errorf("SumNum 期望2,实际%d", resp.SumNum)
	}
	if len(resp.DeviceList.Items) != 2 {
		t.Fatalf("通道数期望2,实际%d", len(resp.DeviceList.Items))
	}
	it := resp.DeviceList.Items[0]
	if it.DeviceID != "34020000001320000019" || it.Name != "Camera1" || !it.IsOnline() {
		t.Errorf("通道1字段不符: %+v", it)
	}
	if resp.DeviceList.Items[1].IsOnline() {
		t.Error("通道2应离线(OFF)")
	}
}

// TestParseCatalogGB2312 T3: GB2312 编码应答也能解析
func TestParseCatalogGB2312(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="GB2312"?><Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000001320000018</DeviceID><SumNum>1</SumNum><DeviceList Num="1"><Item><DeviceID>34020000001320000019</DeviceID><Name>Cam</Name><Status>ON</Status></Item></DeviceList></Response>`)
	resp, err := ParseCatalogResponse(body)
	if err != nil {
		t.Fatalf("GB2312 解析失败: %v", err)
	}
	if len(resp.DeviceList.Items) != 1 {
		t.Errorf("期望1通道,实际%d", len(resp.DeviceList.Items))
	}
}
