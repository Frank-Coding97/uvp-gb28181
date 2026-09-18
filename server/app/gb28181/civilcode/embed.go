// Package civilcode embed 行政区划字典(GB/T 2260 6 位级)
//
// 数据来源:https://github.com/modood/Administrative-divisions-of-China
// 协议:WTFPL(See data/LICENSE.txt)
// 由 tools/convert_civil_code 把 pcas-code.json 转扁平 6 位级,
// 启动期 SeedIfEmpty 幂等写库,后续 service 走进程内缓存。
package civilcode

import _ "embed"

//go:embed data/civil_code_6digit.json
var rawCivilCodeJSON []byte
