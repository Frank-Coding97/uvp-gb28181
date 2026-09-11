-- SIP 首次部署引导重构:废弃 system_installation 状态机
-- 2026-07-20 起,SIP 引导判据只依赖 gb_sip_config 表的存在性,不再需要独立状态字段.
-- 详见 wiki/projects/uvp/architecture/sip-onboarding-redesign.md

DROP TABLE IF EXISTS `system_installation`;
