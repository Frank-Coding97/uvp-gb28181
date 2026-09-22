-- 媒体节点退役：保留「撤销对端 hook」所需的凭据（MySQL 5.7+）。
--
-- 为什么要有这张表：
--   `Registry.Delete` 是**硬删**（删 DB 行 + 删内存索引），而且**不会通知对端**。
--   对端 ZLM 的 hook 配置是平台自己写进去的，平台删行之后它毫不知情，仍按自己的周期
--   回调 ⇒ 平台查不到行 ⇒ 每条回调都记一次 `gb28181.hook.auth.rejected`（node_unknown）。
--   现场实测：220 上一个被移除的 ZLM 心跳每 10s 一条，**8640 行/天**。
--
--   要能撤销，必须留凭据：硬删把 `api_secret` 一起删掉之后，平台既不再知道"这是谁"，
--   也没有任何能用的凭据去把对端的 hook 关掉 —— 于是只能永久忍着它刷屏。
--   所以删除时把**撤销所需的最小集合**（uuid / host / api_port / api_secret）挪到本表，
--   之后：
--     ① 删行前先试着解约（清掉平台写进对端的 managed hook）；
--     ② 删行后若仍收到该 uuid 的回调，命中本表 ⇒ 节流重试解约；
--     ③ 解约成功即 INFO 收尾，之后不再是"未知节点"，也不再刷屏。
--
-- 为什么不是给 meta_node 加 `state='retired'` 软删：
--   `MetaNodeRepo.List` 目前**无任何过滤**（`zlm/repo/node_repo.go`），软删行会直接进
--   注册表 ⇒ 要同步改 List / LoadAll / 调度 / 管理接口 / 健康检查五处以上，
--   而且"一个已退休的节点"混在活跃节点表里，每个读到它的人都要重新判一次。
--   撤销凭据是**独立职责**，独立成表最小侵入。
--
-- ⛔⛔ 建表**必须显式写 COLLATE=utf8mb4_general_ci**：只写 `DEFAULT CHARSET=utf8mb4` 时，
--   MySQL 8.0 会取该字符集的默认排序规则 `utf8mb4_0900_ai_ci`，与本库不一致，
--   第一次 JOIN 就报 1267 "Illegal mix of collations"（2026-09-20 在 gb_channel_snapshot 上踩过）。
--
-- ⛔ 本表的 `api_secret` 与 `meta_node.api_secret` 同等级（都是明文列，同库同权限）；
--   `unprovision_state=unreachable` 的行是"凭据还在但撤不掉"的显式记录，
--   供运维决定人工处理，不要悄悄当成功。
--
-- 幂等：`CREATE TABLE IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS` 守卫，连跑两遍无副作用。

-- retired-media-node-credentials:start

CREATE TABLE IF NOT EXISTS `meta_node_retired` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `media_server_uuid` varchar(64) NOT NULL,
  `name` varchar(64) NOT NULL DEFAULT '',
  `host` varchar(64) NOT NULL DEFAULT '',
  `api_port` int NOT NULL DEFAULT 18080,
  `api_secret` varchar(128) NOT NULL DEFAULT '',
  `retire_reason` varchar(255) NOT NULL DEFAULT '',
  `unprovision_state` varchar(16) NOT NULL DEFAULT 'pending',
  `unprovision_attempts` int NOT NULL DEFAULT 0,
  `last_attempt_at` datetime(6) DEFAULT NULL,
  `retired_at` datetime(6) NOT NULL,
  `updated_at` datetime(6) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_retired_media_server_uuid` (`media_server_uuid`),
  KEY `idx_retired_unprovision_state` (`unprovision_state`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='已退休媒体节点:仅保留撤销对端 hook 所需的凭据';

-- retired-media-node-credentials:end
