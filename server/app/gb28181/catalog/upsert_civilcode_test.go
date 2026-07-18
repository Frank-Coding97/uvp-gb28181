package catalog

import "testing"

// mockCivilCodeLookup 测试用 mock
type mockCivilCodeLookup struct {
	validCodes map[string]bool // 字典里存在的 code
}

func (m *mockCivilCodeLookup) Lookup(code string) interface{} {
	if m.validCodes[code] {
		return &struct{}{} // 返回非 nil 表示存在
	}
	return nil
}

func TestResolveCivilCode(t *testing.T) {
	// 装配 mock 字典(只有 370100 济南 + 370200 青岛有效)
	mock := &mockCivilCodeLookup{
		validCodes: map[string]bool{
			"370100": true, // 济南
			"370200": true, // 青岛
		},
	}
	civilCodeLookup = mock

	tests := []struct {
		name            string
		itemCivilCode   string // L1: XML 上报
		clsCivilCode    string // L2: classifier 提取
		parentCivilCode string // L3: 父节点
		want            string
	}{
		{
			name:            "L1 命中:XML 上报有效",
			itemCivilCode:   "370100",
			clsCivilCode:    "370200",
			parentCivilCode: "370300",
			want:            "370100", // L1 优先
		},
		{
			name:            "L1 无效降 L2:XML 上报但字典查不到",
			itemCivilCode:   "999999", // 不在字典
			clsCivilCode:    "370200", // 字典有
			parentCivilCode: "370300",
			want:            "370200", // 降级 L2
		},
		{
			name:            "L1 空降 L2:XML 未上报",
			itemCivilCode:   "",
			clsCivilCode:    "370100",
			parentCivilCode: "370300",
			want:            "370100", // L2
		},
		{
			name:            "L1/L2 都无效降 L3:父节点",
			itemCivilCode:   "888888", // 不在字典
			clsCivilCode:    "999999", // 不在字典
			parentCivilCode: "370300", // 父节点不校验字典,直接信任
			want:            "370300", // L3
		},
		{
			name:            "L1/L2 空 L3 有效:只有父节点",
			itemCivilCode:   "",
			clsCivilCode:    "",
			parentCivilCode: "370100",
			want:            "370100", // L3
		},
		{
			name:            "全部无效降 L4:兜底 000000",
			itemCivilCode:   "",
			clsCivilCode:    "",
			parentCivilCode: "",
			want:            "000000", // L4 兜底
		},
		{
			name:            "L1 格式错(非 6 位)降 L2",
			itemCivilCode:   "37",    // 长度错
			clsCivilCode:    "370100", // 有效
			parentCivilCode: "",
			want:            "370100", // L2
		},
		{
			name:            "L1 格式错(含非数字)降 L2",
			itemCivilCode:   "37010X", // 非全数字
			clsCivilCode:    "370200",
			parentCivilCode: "",
			want:            "370200", // L2
		},
		{
			name:            "L2 格式错降 L3",
			itemCivilCode:   "",
			clsCivilCode:    "37",    // 长度错
			parentCivilCode: "370100",
			want:            "370100", // L3
		},
		{
			name:            "L3 格式错降 L4",
			itemCivilCode:   "",
			clsCivilCode:    "",
			parentCivilCode: "37", // 长度错
			want:            "000000", // L4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCivilCode(tt.itemCivilCode, tt.clsCivilCode, tt.parentCivilCode)
			if got != tt.want {
				t.Errorf("resolveCivilCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveCivilCode_NoLookup(t *testing.T) {
	// 场景:civilCodeLookup 为 nil(bootstrap 失败,降级)
	civilCodeLookup = nil

	tests := []struct {
		name            string
		itemCivilCode   string
		clsCivilCode    string
		parentCivilCode string
		want            string
	}{
		{
			name:            "lookup nil 时 L1/L2 无法校验,降 L3",
			itemCivilCode:   "370100",
			clsCivilCode:    "370200",
			parentCivilCode: "370300",
			want:            "370300", // L1/L2 因 lookup==nil 跳过,直接 L3
		},
		{
			name:            "lookup nil + 无父节点 → L4",
			itemCivilCode:   "370100",
			clsCivilCode:    "370200",
			parentCivilCode: "",
			want:            "000000", // L4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCivilCode(tt.itemCivilCode, tt.clsCivilCode, tt.parentCivilCode)
			if got != tt.want {
				t.Errorf("resolveCivilCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAllDigit(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"全数字", "370100", true},
		{"含字母", "37010X", false},
		{"空字符串", "", false}, // 空串边界:isAllDigit 定义空串为 false
		{"含空格", "370 100", false},
		{"含符号", "370-100", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAllDigit(tt.s); got != tt.want {
				t.Errorf("isAllDigit(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}
