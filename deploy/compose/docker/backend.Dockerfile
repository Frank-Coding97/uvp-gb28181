# UVP-GB28181 后端镜像 —— 平台主程序（API / SIP 信令 / 业务编排）
#
# ⛔ 基础镜像为什么是 alpine 而不是 debian：
#   后端是 CGO_ENABLED=0 静态链接的 Go 二进制（SQLite 引擎是纯 Go 的 modernc 实现），
#   不依赖任何 libc ⇒ 用 alpine 能省下几十 MB，也少一层系统库版本风险。
#   但 alpine 是 musl，⚠️ **一旦将来打开 CGO 就必须换回 debian**，否则
#   二进制在 alpine 里根本跑不起来（报 "not found"，而不是缺库，极具误导性）。
#
# ⛔ 镜像里**不含** config.yml：它是每套部署独有的（含数据库凭据与端口），
#   由宿主机安装目录 bind 挂载进来（见 compose.yml）。镜像保持"与部署无关"。
FROM alpine:3.22.1

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
ENV TZ=Asia/Shanghai

# ⛔ resource/database 必须进镜像：首次启动要靠它建库（SQLite 基线 / 迁移脚本）。
#   缺了它的症状是「容器起来了、日志在刷，但页面一登录就报未知表」。
# ⛔⛔ 二进制在镜像里**必须叫 /app/uvp-gb28181**，不能叫 uvp-server。
#   升级流程在 Validate 阶段要执行
#     docker compose exec -T backend /app/uvp-gb28181 -upgrade-readiness
#   这个路径是 ComposeAdapter 里的硬约定（systemd 版的二进制名同样是
#   uvp-gb28181，适配器测试里也有断言锁着）。原先这里 COPY 成 uvp-server，
#   升级装到 Validate 就报 `stat /app/uvp-gb28181: no such file or directory`
#   ⇒ 判为"运行时身份校验失败" ⇒ 整个升级回滚。
#   ⛔ 这个坑只有在真跑一次升级时才会暴露：安装、点播、页面全都正常。
#   构建上下文里那份文件名仍是 uvp-server（build-images.sh 拷的），
#   COPY 的目标名与源名不同是允许的，别再改回同名。
COPY uvp-server /app/uvp-gb28181
COPY version.json /app/version.json
COPY resource/database /app/resource/database
COPY resource/public /app/resource/public

# 日志与上传目录在容器里也要先建出来：
# 宿主机卷挂载会覆盖这两个目录，但镜像自带一份能保证"不挂卷也能起来"（排障时很有用）。
RUN mkdir -p /app/resource/logs /app/resource/public/uploads \
    && chmod +x /app/uvp-gb28181

# 后端在 host 网络下监听 51002（端口规划见 deploy/standalone/PORTS.md）。
# ⛔ 这里**只声明业务 API 口**：SIP 口(51064)与 RTP 段(51014-51063)是运行时从数据库
#   读出来的、管理员可改，写进 EXPOSE 就会变成"看起来固定"的错误承诺；
#   而且 host 网络下 EXPOSE 本来也不做任何实际映射，纯属文档性质。
EXPOSE 51002/tcp

ENTRYPOINT ["/app/uvp-gb28181"]
