# ip2region 数据库

`ip2region_v4.xdb` 来自 [lionsoul2014/ip2region](https://github.com/lionsoul2014/ip2region)，随 UVP 发行包提供本地 IPv4 归属地查询。

项目及数据文件按 Apache License 2.0 发布。数据库不是启动必需项；文件缺失或查询不到时，程序会自动尝试内置的 `ipwho.is` 兜底接口，失败则显示“未知”。
