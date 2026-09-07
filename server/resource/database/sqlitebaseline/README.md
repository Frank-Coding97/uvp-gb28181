# SQLite 基线

这是 Windows 单机版使用的受审核 SQLite 基线。`baseline.sql` 来自发布输入
`e07857cc`（`uvp-gb28181.sql` 加四个已接受的 GB28181 迁移），包含 86 张表、
242 个显式索引和 912 条确定性系统种子语句。`manifest.json` 保存输入摘要、完整
表清单和索引清单；`generate.py` 只是这组固定输入的审计辅助工具，不是通用的
MySQL 转 SQLite 转换器。

对这 86 张表的输入审计统计为：234 个 `UNSIGNED` 整数列、500 个带长度的
`VARCHAR` 列、6 个带长度的 `CHAR` 列、1 个 `VARBINARY(64)` 列、2 个 JSON 列、
277 个 `DATETIME` 列和 3 个 `DATE` 列。对应的范围、长度和 JSON 检查都已写进
基线 DDL。

`baseline.go` 暴露嵌入的 `SQL`、版本和锁定 SHA-256。调用方负责在一个事务中执行
整批 SQL 并记录迁移 marker；本文件不创建 marker 表，也不包含 `BEGIN`/`COMMIT`。

基线只播种权限、菜单、API、字典、角色和 SIP 安全策略等系统元数据。它不播种设备、
通道、SIP 配置、级联平台、媒体节点、日志、用户、用户角色或演示凭据。行政区划
数据由 Go 初始化阶段用完整的 3348 条 civil code JSON 播种。

SQLite 映射和已保留的约束如下：

- `DATE`、`DATETIME`、`TIMESTAMP` 保留声明类型，只去掉 MySQL 精度；这是
  `modernc.org/sqlite` 在 `_time_format=sqlite` 下读写 `time.Time` 的必要条件。
  当前输入没有 `TIME` 列；该类型若进入输入会按 TEXT 处理，因为驱动返回的是字符串。
- 整数列声明为 `INTEGER`，并用 `typeof(...)= 'integer'` 防止 REAL 或非数值文本混入。
  signed `TINYINT`/`SMALLINT`/`MEDIUMINT`/`INT` 的上下界、所有可表达的
  `UNSIGNED` 上下界都保留。SQLite 只有有符号 64 位整数，因此 `BIGINT UNSIGNED`
  的基线范围是 `0..9223372036854775807`；超过该范围的数据与单机版不兼容，需要
  单独的数据模型决策。
- `VARCHAR`、`CHAR` 和 `VARBINARY` 映射为 TEXT，并以 CHECK 保留源长度上限；
  VARBINARY 的检查按存储字节长度计算。
- `BLOB`/`MEDIUMBLOB` 保留 SQLite 的 BLOB affinity；当前输入没有其他二进制类型。
- JSON 映射为 TEXT，并以 `json_valid` CHECK 保留有效 JSON 约束；可空列仍接受 NULL。
- 当前输入没有 `ENUM` 列；后续若加入，必须为每个枚举值补充显式 CHECK，不能按普通 TEXT
  静默转换。
- MySQL 排序规则不搬入 SQLite。默认是 BINARY，登录用户名按原始大小写和字节精确匹配，
  所以 `admin` 与 `Admin` 是两个用户名；LIKE 搜索仍遵循 SQLite 自身语义。密码、路径
  等字段不做规范化。
- 重复的 MySQL 索引名按表名前缀改名以满足 SQLite 的 schema 级命名空间；唯一且有
  对外依赖的名称（例如 `username`、`idx_recovery_required`）保持不变。
- MySQL `ON UPDATE CURRENT_TIMESTAMP` 没有伪造为触发器；应用层负责更新 `updated_at`。
  这组输入只涉及更新时间列，若未来代码依赖数据库自动更新时间，应先补充专门决策和回归。

重新生成并核对锁定产物：

```sh
python3 server/resource/database/sqlitebaseline/generate.py
go test ./resource/database/sqlitebaseline
```

生成器会重写 `baseline.sql` 和 `manifest.json`；提交前必须检查 SHA-256、manifest
清单与 SQLite 3.53.4 实际 schema 一致。
