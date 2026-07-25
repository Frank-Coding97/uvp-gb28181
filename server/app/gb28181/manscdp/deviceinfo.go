package manscdp

import (
	"encoding/xml"
	"fmt"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// DeviceInfoQuery DeviceInfo 查询请求(平台→设备)
// GB/T 28181 MANSCDP: <Query><CmdType>DeviceInfo</CmdType>...</Query>
type DeviceInfoQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

// BuildDeviceInfoQuery 构造 DeviceInfo 查询 XML(国标格式,GB2312 声明)
// deviceID = 目标设备国标编码(20 位设备编码);sn = 查询序列号
func BuildDeviceInfoQuery(deviceID string, sn int) ([]byte, error) {
	return BuildDeviceInfoQueryWithProfile(protocol.ProfileFor(protocol.Version2016), deviceID, sn)
}

// BuildDeviceInfoQueryWithProfile builds a DeviceInfo query using the
// profile's actual XML charset and declaration.
func BuildDeviceInfoQueryWithProfile(profile protocol.Profile, deviceID string, sn int) ([]byte, error) {
	q := DeviceInfoQuery{CmdType: CmdDeviceInfo, SN: sn, DeviceID: deviceID}
	return MarshalProfiledXML(profile, q)
}

// DeviceInfoResponse DeviceInfo 应答(设备→平台)
// 承载设备本体元数据:名称/厂商/型号/固件/通道总数,回写 gb_device 用
type DeviceInfoResponse struct {
	XMLName      xml.Name `xml:"Response"`
	CmdType      string   `xml:"CmdType"`
	SN           int      `xml:"SN"`
	DeviceID     string   `xml:"DeviceID"`
	Result       string   `xml:"Result"`
	DeviceName   string   `xml:"DeviceName"`
	Manufacturer string   `xml:"Manufacturer"`
	Model        string   `xml:"Model"`
	Firmware     string   `xml:"Firmware"`
	Channel      int      `xml:"Channel"` // 通道总数(参考值,不强依赖)
}

// ParseDeviceInfoResponse 解析一条 DeviceInfo 应答
func ParseDeviceInfoResponse(body []byte) (*DeviceInfoResponse, error) {
	var r DeviceInfoResponse
	if err := newDecoder(body).Decode(&r); err != nil {
		return nil, fmt.Errorf("解析 DeviceInfo 应答失败: %w", err)
	}
	return &r, nil
}
