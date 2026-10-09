package gormhelper

import (
	"fmt"
	"testing"
)

// 用现场两条紧邻的指纹反查对应代码：
//
//	f45a70b18b6981cb  select ... table=sys_openapi_capability   rows=0（失败前最后一条）
//	ade13374a651ac3a  update ... table=sys_openapi_capability   rows=0（无 WHERE）
//
// 指纹只由「关键字 + ?占位」决定，因此可以把候选语句离线算出来比对，
// 不必再跑一次现场。匹配的语句即真凶所在。
func TestFingerprintReverseLookup(t *testing.T) {
	type cand struct{ name, sql string }
	selectors := []cand{
		{"scope IN ? + deleted_at IS NULL + ORDER BY",
			"SELECT * FROM `sys_openapi_capability` WHERE `scope` IN (?) AND `deleted_at` IS NULL ORDER BY `id` ASC,`sys_openapi_capability`.`id` LIMIT 1"},
		{"scope IN ? + deleted_at IS NULL（无 ORDER）",
			"SELECT * FROM `sys_openapi_capability` WHERE `scope` IN (?) AND `deleted_at` IS NULL LIMIT 1"},
		{"deleted_at IS NULL only",
			"SELECT * FROM `sys_openapi_capability` WHERE `deleted_at` IS NULL LIMIT 1"},
		{"Unscoped: scope = ?",
			"SELECT * FROM `sys_openapi_capability` WHERE `scope` = ? ORDER BY `id` ASC LIMIT 1"},
	}
	const wantSel = "f45a70b18b6981cb"
	fmt.Println("— 匹配 select 指纹", wantSel, "—")
	for _, c := range selectors {
		_, fp := statementSummary(c.sql)
		hit := len(fp) >= len(wantSel) && fp[:len(wantSel)] == wantSel
		fmt.Printf("%s%-46s fp=%s\n", map[bool]string{true: "★ ", false: "  "}[hit], c.name, fp)
	}

	const wantUpd = "ade13374a651ac3a"
	fmt.Println("\n— 匹配 update 指纹", wantUpd, "—")
	updates := []cand{
		{"无 WHERE（Model(&零值)）", "UPDATE `sys_openapi_capability` SET `status`=?,`updated_by`=?,`updated_at`=?"},
		{"无 WHERE，仅 status+updated_by", "UPDATE `sys_openapi_capability` SET `status`=?,`updated_by`=?"},
	}
	for _, c := range updates {
		_, fp := statementSummary(c.sql)
		hit := len(fp) >= len(wantUpd) && fp[:len(wantUpd)] == wantUpd
		fmt.Printf("%s%-46s fp=%s\n", map[bool]string{true: "★ ", false: "  "}[hit], c.name, fp)
	}
}