//go:build windows

package main

import (
	"strings"
	"testing"
)

// TestExtractEntryURL 覆盖从 ctl.ps1 entry-url 的 stdout 中提取地址。
// 真实 stdout 可能混入 PowerShell 的 Warning 行（坏配置时 Get-Config 会写warn），
// 所以只看 UVP_ENTRY_URL= 前缀那一行，且取第一行。
func TestExtractEntryURL(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "plain http",
			out:  "UVP_ENTRY_URL=http://192.168.1.10:15500\r\n",
			want: "http://192.168.1.10:15500",
		},
		{
			name: "https with warning noise",
			out:  "警告: 某些配置无效\r\nUVP_ENTRY_URL=https://10.0.0.5:15501\r\n",
			want: "https://10.0.0.5:15501",
		},
		{
			name: "localhost fallback",
			out:  "UVP_ENTRY_URL=http://127.0.0.1:15500\n",
			want: "http://127.0.0.1:15500",
		},
		{
			name: "no marker",
			out:  "something else\n",
			want: "",
		},
		{
			name: "empty",
			out:  "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractEntryURL(tc.out); got != tc.want {
				t.Fatalf("extractEntryURL()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestFindPowerShellShape(t *testing.T) {
	// findPowerShell 依赖目标机环境，这里只保证它不 panic 且返回绝对路径或空串。
	got := findPowerShell()
	if got != "" && !strings.Contains(got, "powershell") {
		t.Fatalf("findPowerShell()=%q 不像 PowerShell 路径", got)
	}
}
