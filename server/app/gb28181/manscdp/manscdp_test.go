package manscdp

import "testing"

// TestParseKeepalive T5-测2: 解析 Keepalive MANSCDP,提取 CmdType/SN/DeviceID
func TestParseKeepalive(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Notify>
<CmdType>Keepalive</CmdType>
<SN>123</SN>
<DeviceID>34020000001320000001</DeviceID>
<Status>OK</Status>
</Notify>`)
	head, err := ParseHead(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !head.IsKeepalive() {
		t.Errorf("期望 Keepalive,实际 CmdType=%s", head.CmdType)
	}
	if head.SN != "123" {
		t.Errorf("期望 SN=123,实际 %s", head.SN)
	}
	if head.DeviceID != "34020000001320000001" {
		t.Errorf("期望 DeviceID=34020000001320000001,实际 %s", head.DeviceID)
	}
}

// TestParseGB2312 T5-测2扩展: GB2312 编码的 XML 也能解析(国标设备常用)
func TestParseGB2312(t *testing.T) {
	// GB2312 声明的 Keepalive(无中文字符,验证 decoder 不报错)
	body := []byte(`<?xml version="1.0" encoding="GB2312"?>
<Notify><CmdType>Keepalive</CmdType><SN>1</SN><DeviceID>34020000001320000001</DeviceID></Notify>`)
	head, err := ParseHead(body)
	if err != nil {
		t.Fatalf("GB2312 解析失败: %v", err)
	}
	if !head.IsKeepalive() {
		t.Errorf("GB2312 期望 Keepalive,实际 %s", head.CmdType)
	}
}

// TestParseMalformed T5-测3支撑: 畸形 XML 返回错误(由 handler 兜底回200)
func TestParseMalformed(t *testing.T) {
	_, err := ParseHead([]byte("not-xml-at-all"))
	if err == nil {
		t.Error("畸形 XML 期望返回错误")
	}
}

// TestParseNonKeepalive: Catalog 等非心跳消息正确识别
func TestParseNonKeepalive(t *testing.T) {
	body := []byte(`<?xml version="1.0"?><Response><CmdType>Catalog</CmdType><SN>5</SN><DeviceID>34020000001320000001</DeviceID></Response>`)
	head, err := ParseHead(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if head.IsKeepalive() {
		t.Error("Catalog 不应被识别为 Keepalive")
	}
	if head.CmdType != "Catalog" {
		t.Errorf("期望 Catalog,实际 %s", head.CmdType)
	}
}
