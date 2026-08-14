// Package catalog 国标多级目录入库管道(plan §3 思路 B+)
//
// 主要职责:
//  1. classify    — 20 位国标编码识别节点类型(channel / device / biz_group / virtual_org)
//  2. anomaly     — 不规范编码兜底,写 gb_anomaly_record + 标记节点
//  3. path_builder — 计算物化路径(/12/47/189/),加速子树查询
//  4. upsert       — gb_catalog_node + gb_channel_mount 入库(N:N 多挂载)
//  5. pipeline     — Ingest(全量) / IngestDelta(增量 Subscribe NOTIFY)
package catalog

import (
	"strings"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// Classification classifier 输出
type Classification struct {
	NodeType  gbmodels.NodeType
	Anomaly   bool
	Reason    string // anomaly 原因
	RawCode   string // 原始编码(anomaly 留痕)
	CivilCode string // 6 位行政区码(从前 6 位提取)
}

// Classify 20 位国标编码 → NodeType(GB/T 28181-2016 §10.2 编码规则)
//
// 编码结构:
//
//	位 1-8   省 4 + 市 4(GB/T 2260 行政区划)
//	位 9-10  行业码(00=通用)
//	位 11-13 类型码:
//	    117 报警控制器 / 130 HVR / 131 摄像机 / 132 IPC
//	    134 报警输入设备 / 135、140 报警输出设备
//	    111 / 112 / 113 / 114 系列设备
//	    200 国标设备
//	    215 业务分组(逻辑分组)
//	    216 虚拟组织(子目录,可嵌套)
//	位 14-20 序列号
//
// 不规范编码(< 20 位 / 非全数字 / 厂商私有前缀 / 类型码未知)→ 兜底为 virtual_org + anomaly=true
func Classify(code string) Classification {
	out := Classification{RawCode: code}

	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		out.NodeType = gbmodels.NodeTypeVirtualOrg
		out.Anomaly = true
		out.Reason = "empty code"
		return out
	}

	// 厂商私有前缀(非数字 / 含字母 / 含分隔符)→ 兜底
	if !isAllDigit(trimmed) {
		out.NodeType = gbmodels.NodeTypeVirtualOrg
		out.Anomaly = true
		out.Reason = "non-numeric code (vendor private)"
		return out
	}

	// 长度异常 → 兜底
	if len(trimmed) != 20 {
		out.NodeType = gbmodels.NodeTypeVirtualOrg
		out.Anomaly = true
		out.Reason = "length != 20 (got " + itoa(len(trimmed)) + ")"
		return out
	}

	// 前 6 位行政区码(GB/T 2260)
	out.CivilCode = trimmed[:6]

	// 类型码 11-13 位(0-indexed: [10:13])
	classifyTypeCode(&out, trimmed[10:13])
	return out
}

// nodeTypeByCode 国标类型码 → 节点类型映射表(独立维护,校验与分类可分别测试)
var nodeTypeByCode = map[string]gbmodels.NodeType{
	"131": gbmodels.NodeTypeChannel, "132": gbmodels.NodeTypeChannel,
	"117": gbmodels.NodeTypeDevice, "130": gbmodels.NodeTypeDevice,
	"134": gbmodels.NodeTypeAlarmInput,
	"135": gbmodels.NodeTypeAlarmOutput, "140": gbmodels.NodeTypeAlarmOutput,
	// 系列编码 → 视为通道(部分厂商把 IPC 当通道挂在 NVR 下)
	"111": gbmodels.NodeTypeChannel, "112": gbmodels.NodeTypeChannel,
	"113": gbmodels.NodeTypeChannel, "114": gbmodels.NodeTypeChannel,
	"115": gbmodels.NodeTypeChannel, "116": gbmodels.NodeTypeChannel,
	"118": gbmodels.NodeTypeChannel, "119": gbmodels.NodeTypeChannel,
	"120": gbmodels.NodeTypeChannel, "121": gbmodels.NodeTypeChannel,
	"200": gbmodels.NodeTypeDevice,
	"215": gbmodels.NodeTypeBizGroup,
	"216": gbmodels.NodeTypeVirtualOrg,
}

func classifyTypeCode(out *Classification, typeCode string) {
	if nodeType, ok := nodeTypeByCode[typeCode]; ok {
		out.NodeType = nodeType
		return
	}
	// 未知类型码:兜底但记录 raw
	out.NodeType = gbmodels.NodeTypeVirtualOrg
	out.Anomaly = true
	out.Reason = "unknown type code: " + typeCode
}

// IsCivilCodeNode 6 位 GB/T 2260 行政区码节点(纯行政区,非 20 位国标)
// 国标 Catalog 里 CivilCode 字段单独提到的行政区(无 device/channel 对应)走这个路径
func IsCivilCodeNode(code string) bool {
	if len(code) != 6 {
		return false
	}
	return isAllDigit(code)
}

// isAllDigit 是否全数字
func isAllDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// itoa 局部 int 转 string(避免 strconv import,classifier 单文件可读)
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
