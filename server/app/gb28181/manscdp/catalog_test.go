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
		<Parental>0</Parental>
		<ParentID>34020000001320000018</ParentID>
		<BusinessGroupID>34020000002150000001</BusinessGroupID>
		<Address>FrontDoor</Address>
		<RegisterWay>1</RegisterWay>
		<Secrecy>0</Secrecy>
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
	if it.BusinessGroupID != "34020000002150000001" || it.ParentID == "" || it.Address != "FrontDoor" || it.Parental != 0 {
		t.Errorf("2022 目录关系字段不符: %+v", it)
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

// TestParseCatalogItemPTZTypeAtItemLevelIsStillAccepted 兼容把 PTZType 写在 Item 层的
// **非合规**设备 —— 旧实现只认这个位置,不能因为补了标准位置就把它弄丢。
func TestParseCatalogItemPTZTypeAtItemLevelIsStillAccepted(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000001320000018</DeviceID><SumNum>1</SumNum>
<DeviceList Num="1"><Item>
<DeviceID>34020000001320000019</DeviceID><Name>Legacy</Name><Status>ON</Status>
<PTZType>4</PTZType>
</Item></DeviceList></Response>`)
	resp, err := ParseCatalogResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := resp.DeviceList.Items[0].PTZType; got != 4 {
		t.Errorf("Item 层 PTZType 期望 4,实际 %d", got)
	}
}

// TestParseCatalogInfo2022 2022 形态:通道属性在 <Info> 容器内,且 BusinessGroupID 在 Item 层。
//
// ⛔ 本用例是**回归锚点** —— 原实现把 PTZType 声明在 Item 层,而 Go encoding/xml 只匹配
// 直接子元素、不递归 <Info> → 合规设备上报的 PTZType 永远读不到,gb_channel.ptz_type 恒为 0。
func TestParseCatalogInfo2022(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000001320000018</DeviceID><SumNum>1</SumNum>
<DeviceList Num="1"><Item>
<DeviceID>34020000001320000019</DeviceID><Name>Cam2022</Name>
<BusinessGroupID>34020000002150000001</BusinessGroupID>
<IPAddress>192.168.10.52</IPAddress><Port>5060</Port>
<Parental>0</Parental><Status>ON</Status>
<Info>
<PTZType>7</PTZType>
<RoomType>2</RoomType>
<SupplyLightType>3</SupplyLightType>
<DirectionType>1</DirectionType>
<Resolution>1920*1080</Resolution>
<PhotoelectricImagingType>2</PhotoelectricImagingType>
<CapturePositionType>1</CapturePositionType>
<StreamNumberList>0</StreamNumberList>
<SSVCRatioSupportList>1</SSVCRatioSupportList>
</Info>
</Item></DeviceList></Response>`)
	resp, err := ParseCatalogResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	it := resp.DeviceList.Items[0]
	if it.PTZType != 7 {
		t.Errorf("PTZType 期望 7(2022 值域 1-7),实际 %d —— <Info> 未被解析", it.PTZType)
	}
	if it.IPAddress != "192.168.10.52" || it.Port != 5060 {
		t.Errorf("Item 层 IPAddress/Port 不符: %q / %d", it.IPAddress, it.Port)
	}
	if it.BusinessGroupID != "34020000002150000001" {
		t.Errorf("2022 的 BusinessGroupID 应在 Item 层取到,实际 %q", it.BusinessGroupID)
	}
	info := it.InfoOrEmpty()
	if info.RoomType != "2" || info.SupplyLightType != "3" || info.DirectionType != "1" || info.Resolution != "1920*1080" {
		t.Errorf("共有属性不符: %+v", info)
	}
	if info.PhotoelectricImagingType != "2" || info.CapturePositionType != "1" ||
		info.StreamNumberList != "0" || info.SSVCRatioSupportList != "1" {
		t.Errorf("2022 独有属性不符: %+v", info)
	}
}

// TestParseCatalogInfo2016 2016 形态:PTZType / PositionType / UseType / BusinessGroupID
// **全部在 <Info> 内**(2022 删掉了 PositionType/UseType 并把 BusinessGroupID 上提)。
//
// ⛔ 本条是 B-1 暴露的现实回归的锚点:模拟器按 2016 标准把 BusinessGroupID 写在 <Info> 内,
// 原实现只读 Item 层 → it.BusinessGroupID 为空 → catalog/pipeline.go 的
// resolveBusinessParent 选不到业务父节点,目录树挂载静默退化。
func TestParseCatalogInfo2016(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000001320000018</DeviceID><SumNum>1</SumNum>
<DeviceList Num="1"><Item>
<DeviceID>34020000001320000019</DeviceID><Name>Cam2016</Name>
<Parental>0</Parental><Status>ON</Status>
<Info>
<PTZType>2</PTZType>
<RoomType>1</RoomType>
<SupplyLightType>1</SupplyLightType>
<Resolution>1280*720</Resolution>
<PositionType>8</PositionType>
<UseType>1</UseType>
<BusinessGroupID>34020000002150000001</BusinessGroupID>
</Info>
</Item></DeviceList></Response>`)
	resp, err := ParseCatalogResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	it := resp.DeviceList.Items[0]
	if it.PTZType != 2 {
		t.Errorf("2016 的 PTZType 期望 2(值域 1-4),实际 %d", it.PTZType)
	}
	if it.BusinessGroupID != "34020000002150000001" {
		t.Errorf("2016 的 BusinessGroupID 在 <Info> 内也必须取到,实际 %q —— 业务分组挂载会静默失效", it.BusinessGroupID)
	}
	info := it.InfoOrEmpty()
	if info.PositionType != "8" || info.UseType != "1" {
		t.Errorf("2016 独有字段(PositionType/UseType)不符: %+v —— 不得因实现 2022 而丢弃", info)
	}
	if info.RoomType != "1" {
		t.Errorf("RoomType 期望 1(室外),实际 %q", info.RoomType)
	}
}

// TestParseCatalogItemInfoTakesPrecedenceOverItemLevel 两处都写了 PTZType 时,
// 以标准位置(<Info>)为准。
func TestParseCatalogItemInfoTakesPrecedenceOverItemLevel(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000001320000018</DeviceID><SumNum>1</SumNum>
<DeviceList Num="1"><Item>
<DeviceID>34020000001320000019</DeviceID><Status>ON</Status>
<PTZType>3</PTZType>
<Info><PTZType>6</PTZType></Info>
</Item></DeviceList></Response>`)
	resp, err := ParseCatalogResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := resp.DeviceList.Items[0].PTZType; got != 6 {
		t.Errorf("<Info> 应优先于 Item 层,期望 6,实际 %d", got)
	}
}

// TestParseCatalogItemMissingInfoAndNonNumericPTZType 边界:<Info> 缺失、PTZType 为空 /
// 非数字(2022 把 PTZType 声明成 string,理论上可能出现非数字)都不应报错,统一归一为 0。
func TestParseCatalogItemMissingInfoAndNonNumericPTZType(t *testing.T) {
	cases := []struct {
		name string
		info string
		// infoEmpty 表示该用例的 <Info> 归一化后应为零值(即容器缺失或内部没有有效内容)。
		infoEmpty bool
	}{
		{name: "无 Info", info: "", infoEmpty: true},
		{name: "PTZType 为空", info: "<Info><PTZType></PTZType></Info>", infoEmpty: true},
		{name: "PTZType 非数字", info: "<Info><PTZType>dome</PTZType></Info>"},
		{name: "Info 为空容器", info: "<Info></Info>", infoEmpty: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Response><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>34020000001320000018</DeviceID><SumNum>1</SumNum>
<DeviceList Num="1"><Item>
<DeviceID>34020000001320000019</DeviceID><Status>ON</Status>` + tc.info + `
</Item></DeviceList></Response>`)
			resp, err := ParseCatalogResponse(body)
			if err != nil {
				t.Fatalf("不应因 %s 报错: %v", tc.name, err)
			}
			// 非数字原样保留在 Info 里(不丢数据),但归一化后的数字字段必须是 0
			if got := resp.DeviceList.Items[0].PTZType; got != 0 {
				t.Errorf("%s: PTZType 期望 0,实际 %d", tc.name, got)
			}
			got := resp.DeviceList.Items[0].InfoOrEmpty()
			if tc.infoEmpty && got != (CatalogInfo{}) {
				t.Errorf("%s: InfoOrEmpty 期望零值,实际 %+v", tc.name, got)
			}
			if !tc.infoEmpty && got.PTZType != "dome" {
				t.Errorf("%s: 非数字原值应保留在 Info 中,实际 %+v", tc.name, got)
			}
		})
	}
}

// TestParseCatalogNotifyNormalizesInfo 订阅路径(catalog NOTIFY)同样必须归一化 ——
// 只修 Response 路径会让订阅来的目录项继续丢掉标准位置的字段。
func TestParseCatalogNotifyNormalizesInfo(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Notify><CmdType>Catalog</CmdType><SN>2</SN><DeviceID>34020000001320000018</DeviceID><SumNum>1</SumNum>
<DeviceList Num="1"><Item>
<DeviceID>34020000001320000019</DeviceID><Name>Cam</Name><Status>ON</Status><Event>ADD</Event>
<Info><PTZType>5</PTZType><RoomType>2</RoomType><BusinessGroupID>34020000002150000001</BusinessGroupID></Info>
</Item></DeviceList></Notify>`)
	notify, err := ParseCatalogNotify(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	it := notify.DeviceList.Items[0]
	if it.PTZType != 5 {
		t.Errorf("NOTIFY 路径的 PTZType 期望 5,实际 %d", it.PTZType)
	}
	if it.BusinessGroupID != "34020000002150000001" {
		t.Errorf("NOTIFY 路径的 BusinessGroupID 期望取到 <Info> 值,实际 %q", it.BusinessGroupID)
	}
}
