# Redis Windows 行为 probe

这是 T02 的独立验证器。它只启动、连接和终止自己创建的 `redis-server` 子进程，使用标准库 RESP2 客户端，不修改服务端业务接口，也不依赖 WSL、Docker 或 CGO。

## 构建

在本目录执行：

```sh
go test -race ./...
go vet ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o redis-probe.exe .
```

## 运行

`-redis-server` 是唯一必需参数。密码由 probe 生成并写入临时配置文件，不放在进程参数中；工作目录自动包含中文和空格，端口随机选择并只绑定 `127.0.0.1`。

```powershell
.\redis-probe.exe `
  -redis-server 'D:\UVP\runtime\redis-server.exe' `
  -expected-version '7.2.16' `
  -source-sha '335554f18caf7bbf6b0ac2b3548133d750f00a1b' `
  -license-file 'D:\UVP\licenses\redis-COPYING' `
  -disk-full-root 'V:\'
```

标准输出是单个 JSON 报告，包含 `t02-a`、`t02-b`、`t02-c` 三个独立结果、Redis 版本、可执行文件 SHA-256、临时目录、AOF 重写/恢复轮次、OOM/noeviction 结果和许可证文件 SHA-256。任一失败返回非零；未提供 `-disk-full-root` 时磁盘满注入标记为 `not_executed`，因此即使其它 C 项通过，整体也会保持非零，不能把未执行当作通过。

磁盘满测试只接受原生 Windows 的卷根路径，且卷标必须精确为 `UVP_P0_TEST`、卷总容量不得超过 128 MiB。probe 会在该卷创建随机工作目录和填充目录，填充到系统明确返回 ENOSPC 后验证 AOF 写入被明确拒绝，再删除填充文件、重启本次 Redis 并检查已确认数据。普通系统盘、无标记卷、路径子目录和超限卷都会在写入前拒绝；其它系统仍保持 `not_executed`。磁盘满注入需要主线程先创建并挂载专用小卷，probe 不创建、格式化、挂载或卸载卷。

`-source-sha` 记录锁定的源码提交，源码和子模块是否与锁定清单一致仍由 `deploy/windows/source_lock.py` 验证；probe 不把“二进制 SHA 相同”推断成“由指定源码构建”。许可证参数同样只核对指定物料存在且非空，并输出其哈希，正式物料仍需与锁定源码一起归档。

本机若安装了 `redis-server`，`go test ./...` 会启动隔离实例做真实集成测试；没有真实服务端时只运行协议层测试并跳过该集成用例。测试用的 Redis 版本不代表 Windows 7.2.16 验收结果。
