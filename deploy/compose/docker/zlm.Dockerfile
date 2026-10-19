# UVP-GB28181 流媒体镜像 —— ZLMediaKit（二开版）
#
# ⛔ 基础镜像 debian:12-slim 的两个理由：
#   ① MediaServer 是 **glibc 动态链接**的（在 centos7 / glibc 2.17 底座上编的，
#      向前兼容到 glibc 2.36 没问题），alpine 是 musl ⇒ 直接跑不起来；
#   ② 它还要系统的 libz.so.1（ffmpeg 的 .so 自带在 lib/ 里，但 zlib 不是）。
#
# ⛔ 用随包分发那份**已验证过**的二进制，不在镜像里重新编译 ZLM：
#   重编一次的耗时是几十分钟，且编译参数差异会让「绿色包能跑、容器跑不了」
#   这类问题完全无法归因。同一份二进制 = 同一套行为。
FROM debian:12-slim

# curl 是给容器健康检查用的：debian 的 /bin/sh 是 dash，
# ⛔ 不支持 bash 的 /dev/tcp 特性（`exec 3<>/dev/tcp/127.0.0.1/51004` 在 dash 里直接失败），
#    所以健康探测只能用真实的网络工具，不能图省事写 /dev/tcp。
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata zlib1g curl \
    && rm -rf /var/lib/apt/lists/*

ENV TZ=Asia/Shanghai \
    LD_LIBRARY_PATH=/opt/zlm/lib

WORKDIR /opt/zlm

# ⛔ 整个 zlm/ 一起拷：MediaServer 依赖 lib/ 下的 ffmpeg .so 且是**相对位置**，
#   逐个拷容易漏；另外 default.pem 在部分版本里并不存在（建包脚本是"有才拷"），
#   写成必拷的 COPY 会直接让构建失败。
COPY zlm/ /opt/zlm/
COPY zlm-entrypoint.sh /usr/local/bin/uvp-zlm-entrypoint.sh

# ⛔⛔ 原始 www 放在 www.default，运行时目录 www 由 entrypoint 首次填充：
#   www 是**录像/截图/HLS 切片的落盘目录**（config.ini 里 mp4_save_path/hls_save_path
#   都是 ./www 这种相对路径 ⇒ 相对 WORKDIR /opt/zlm），必须挂宿主机卷才能持久化。
#   而挂卷会把镜像里的 www 整个盖掉 ⇒ 必须先有一份"种子"留在外面，
#   否则容器一挂卷就得到空目录，ZLM 的 Web 管理页与默认资源全没了。
COPY zlm/www /opt/zlm/www.default

# ⛔⛔ 运行时**会写**的两个目录必须预先建好且可写：
#   · /opt/zlm/log    —— ZLM 默认日志目录（相对 WORKDIR 的 ./log）。
#        镜像里没有它时，非 root 身份下 `mkdir` 会 `permission denied`，
#        而 ZLM 的处理是**每秒重试一次、永不退出**（不是启动失败），
#        表现为容器一直 Restarting、日志刷满一样的 warning。
#   · /opt/zlm/ffmpeg —— config.ini 的 ffmpeg.log 落点。
#   用 0777 而不是 chown <uid>：业务 UID 是**运行时**由 UVP_BUSINESS_UID 决定的，
#   构建期写死一个数字，客户改了 UID 就会再次踩同一个坑。
#   这两个目录只放日志/临时文件，不承载任何敏感数据。
#   · /opt/zlm/run  —— **必须可写**：entrypoint 要在里面放 config.ini 的副本并改写端口。
#        不能直接改 /opt/zlm/config.ini：那个文件属主是 root（镜像层），
#        非 root 容器写它会 `cannot create config.ini.tmp: Permission denied`
#        ⇒ 端口改写静默失效 ⇒ ZLM 还在 51004 而后端以为在 53004。
RUN chmod +x /opt/zlm/MediaServer /usr/local/bin/uvp-zlm-entrypoint.sh \
    && mkdir -p /opt/zlm/ffmpeg /opt/zlm/log /opt/zlm/run \
    && chmod 0777 /opt/zlm/ffmpeg /opt/zlm/log /opt/zlm/run

# ZLM 的端口（51004-51013 各协议 + 51014-51063 RTP 段）已在随包 config.ini 里写好。
# ⛔ 这里不 EXPOSE 那一整段：host 网络下 EXPOSE 不产生任何实际映射，
#   写 50 个端口只会让 `docker ps` 的输出变得没法看。
EXPOSE 51004/tcp

ENTRYPOINT ["/usr/local/bin/uvp-zlm-entrypoint.sh"]
