# 验证记录

## 1.0.7 当前状态与原生界面

取消路由器日志文件、轮转和 logger 输出，旧日志在升级中删除；只原子覆盖当前状态 JSON，读取和写入都限制在 1 KiB 内。单元测试覆盖覆盖写入、重复状态不重写、控制字符处理、并发写入、无临时/历史文件及坏状态拒绝，race/vet 通过。

真实 namespace + Merlin ash 集成测试验证连接后 state=connected，断线更新 connect_error，错密钥更新 auth_error，恢复后 connected，关闭后 disabled；整个过程没有 daemon.log/control.log，数据路径与原有清理检查通过。服务端安装单元 StandardOutput/StandardError=null，自启/卸载测试通过。

页面使用 ASUS 原生 FormTitle/FormTable、开关、配置/帮助页签和提交按钮。Node 回归覆盖轮询保留编辑、RPC、六种状态、密码隐藏、失败校验和无日志面板。本地浏览器使用真实 388 CSS 及 fixture API 进行视觉检查，配置和帮助页签切换正常；这是开发预览，不能替代固件实机 API/主题测试。

## 1.0.6 路由器日志限制

原实现仅在 daemon 启动前检查 daemon.log，持续断线重连不会触发；control.log 无上限。改为已有 supervisor 在子进程运行时每 60 秒检查两份日志，超过 256 KiB 时原地保留末尾 128 KiB，控制日志另在每次写入后检查。未增加 cron 或独立监护进程。

真实 namespace + Merlin 配置的 BusyBox ash 集成测试将周期加速为 1 秒，在隧道运行时给两份日志追加超过阈值的数据；验证保留末尾标记、大小降到约 128 KiB、inode 不变。随后实际断线/重连的新记录仍写入可见日志，其他转发与停止/卸载测试通过。直接子进程退出后的 ash 轮询可正常结束，wait 能取回退出码，TERM 响应约一秒。

## 1.0.5 软件中心卡片

使用软件中心前端相同的注册解析逻辑重现：`softcenter_module_icmp_hijack_install` 被读成模块 `icmp` 的 `hijack_install`，由于没有 `install` 字段被删掉。Node 回归测试确认实际新版安装器生成 `icmphijack` 的卡片，并匹配页面、图标及卸载入口。

在精简 BusyBox fixture 中模拟已安装 1.0.4 的真实旧卸载脚本及注册布局，原地修复执行两次均通过，配置文件字节和启用状态不变；从新卡片对应卸载入口卸载后，新旧 metadata、页面、图标、入口、配置和进程/规则均清理。新版独立安装及 HND 离线入口检查也通过。

```sh
node router/tests/verify_center_registry.js
```

## 1.0.4 梅林 ash 命令检测

对照用户指定的 fancyss 安装脚本，核对 Merlin 388 BusyBox 配置，确认 `ASH_CMDCMD` 关闭。此前测试只使用常规 Linux shell，未覆盖这个差异，导致 `command -v` 将已有工具误报为缺失。

从 BusyBox 1.25.1 官方源码构建测试 shell，匹配梅林相关 shell 开关（关闭 command、job control、getopts、内建 echo/printf/test、64 位 shell 算术）。测试 shell 在构建缓存中，不随插件发布。`tools/build_merlin_test_shell.py` 可重建。

该 shell 中旧检测对已有 iptables 精确复现误报；新检查验证可执行文件、符号链接、绝对路径，正确拒绝不存在、不可执行和目录。两种安装/卸载的完整 fixture 通过，含 dbus 模式及 IPv6 链验证。控制脚本和 supervisor 均使用此 ash 的真实五 namespace 测试通过：逐跳、客户端隔离、断线保护、重连、重载及清理正常。

```sh
ICMPTUNNEL_TEST_SHELL=/path/to/busybox-1.25.1/busybox \
ICMPTUNNEL_TEST_BUSYBOX=1 python3 router/tests/verify_command_lookup.py

ICMPTUNNEL_TEST_SHELL=/path/to/busybox-1.25.1/busybox \
ICMPTUNNEL_TEST_BUSYBOX=1 python3 router/tests/verify_scripts.py

ICMPTUNNEL_TEST_SHELL=/path/to/busybox-1.25.1/busybox \
ICMPTUNNEL_TEST_BUSYBOX=1 sudo -E python3 tests/integration.py
```

## 1.0.3 无外部 ip 环境

去掉路由器外部 `ip` 依赖，统一命令路径。安装/卸载 fixture 的 `id` 和 `ip` 都返回 127 并记录调用，两种安装模式的完整流程通过，两个记录文件都未创建。

内置 IPv4 rtnetlink 助手的真实隔离 namespace 测试与 iproute2 对照通过：规则/路由新增、替换、查询、删除；同 pref 下不同 mark、mask、table 的其他规则保持不动，同表不同 metric 的路由保持不动。Go race、vet 和 ShellCheck 通过。

五 namespace 数据路径使用真实控制脚本和原生内置助手通过完整测试；控制脚本 PATH 最前放置会返回 127 的 `ip`，从启动到关闭都没有调用它。逐跳、多个客户端、断线保护、重连和卸载验证正常。

ARMv7/ARMv8 的请求编码、解析和静态编译通过，ARM daemon + 原生内置助手的数据路径测试通过。QEMU user 8.2 的 [消息翻译实现](https://github.com/qemu/qemu/blob/v8.2.2/linux-user/fd-trans.c#L1379-L1421) 不支持 RTM_*RULE，直接执行 ARM 规则查询会被模拟器拒绝，所以不能将这次模拟测试描述为 ARM 路由系统调用的实机验证。此前纯 ARM 全流程记录对应使用固件 `ip` 的旧版本。

## 1.0.2 精简固件与 GitHub 部署

修复实际固件 `id: not found` 后的权限误判：安装/卸载通过 shell 内建读取 `/proc/self/status` 的有效 UID。新增生命周期 fixture 的 `id` 命令会返回 127 并记录调用；两种安装方式均成功，且记录文件未创建。非 root 有效 UID 在修改文件前被拒绝，真实 UID 与有效 UID 分别测试。shell 语法及 ShellCheck 通过。

新增 GitHub bootstrap 的七个隔离测试，验证私有 API 鉴权和下载类型、最新/固定 tag、参数转发、错误 SHA256、缺少或重复资产、错误 JSON/网络失败、安装退出码、临时文件清理，以及安装前删除 Token 环境变量。

真实 WSL systemd 安装检查确认服务 active 且 is-enabled=enabled；重复安装保留配置。删除临时发行目录后，持久卸载脚本仍可使用，并清理自身、配置、程序、服务及自启链接；用户额外文件被保留。测试结束恢复基线环境，不输出生成的密钥。

## 1.0.1 离线包平台识别修复

1.0.0 的打包和生命周期模拟没有覆盖软件中心离线入口，遗漏了 `.valid`，导致真机在运行插件脚本前拒绝安装。1.0.1 添加根目录隐藏文件 `.valid`（字节为 `hnd\n`），构建前和压缩包校验都检查它。

新增 `python3 router/tests/verify_offline_package.py`，在临时目录执行固定 2023/2026 上游离线入口的只读条件摘录，不执行安装器清理/安装动作。实际旧包在两版条件中均重现用户的 ASUS hnd 拒绝日志；实际新包在 AX86U 388 / AX5400 两组入口条件中通过。缺失或错误标识、缺失页面/脚本目录、安装脚本出现禁词均正确拒绝。

1.0.1 实际压缩包隐藏文件、架构、权限、版本与 SHA256 校验通过，两种安装方式的生命周期模拟和 ShellCheck 通过。此次仅改动包平台声明和版本；隧道协议与既有服务端兼容，下方网络测试记录仍适用。

## 初版数据路径验证

日期：2026-10-06。宿主为 Windows / WSL2 Ubuntu 24.04，Linux 6.6.87.2；构建工具链为 Go 1.27.1。未连接华硕实机。

## Go 与静态脚本检查

```sh
CGO_ENABLED=1 go test -race ./...
go vet ./...
shellcheck router/install.sh router/uninstall.sh router/scripts/*.sh server/*.sh
python3 router/tests/verify_scripts.py
```

全部通过。race 测试覆盖服务端包匹配、包转换与加密流；Wire 测试包括双向真实握手、随机会话密钥、错密钥/错证明、最大帧、长度 AAD、重复帧、篡改、截断、计数耗尽、并发写入和短写。

Packet 测试覆盖请求 TTL 与 ID 改写、已知 checksum 向量、多个客户端、IP options、截断 ICMP 错误引用、RFC4884 扩展、低 TTL 回包和结构错误。服务端测试验证第二跳、自身目的地址、损坏回包丢弃、ID/sequence/目标/payload 匹配和过期映射。

路由器隔离脚本测试使用实际 shell 脚本与 mock 的 nvram/dbus/iptables/ip，不操作宿主防火墙。验证两种安装模式、规则重入、注入 MARK 失败保留 DROP guard、已有策略优先级冲突不删除对方规则、配置校验、0600 权限、hooks 内容与模式保留、dbus 和自有文件清理。

## 五个 namespace 的实际网络测试

```sh
sudo python3 tests/integration.py
```

使用实际 Go daemon 和路由器控制脚本，把控制脚本里的固定安装/临时目录替换成独立测试目录；网络规则仍由真实脚本执行。

```text
PC 192.168.50.100
  -> router 192.168.50.1
  == TCP == server 10.10.0.2
  -> gateway 10.20.0.2
  -> destination 10.30.0.2
```

路由器额外配置全局 rp_filter=1 和原有 MASQUERADE 规则，确保回包路径检查和源地址保留经过测试。真实输出之一：

```text
traceroute to 10.30.0.2 (10.30.0.2), 6 hops max, 60 byte packets
 1  192.168.50.1  0.023 ms
 2  10.10.0.2  0.465 ms
 3  10.20.0.2  0.552 ms
 4  10.30.0.2  0.574 ms
```

通过项目：

1. 电脑 Ping 路由器、目标及服务器；逐跳地址顺序正确。
2. 两个 LAN 源地址同时使用 echo ID=4242 和相同 sequence，全部正常回应。
3. 开启时 ICMPv6 转发丢弃，本地 IPv6 Ping 和 NDP 正常；关闭后远端 IPv6 Ping 恢复。
4. 普通 TCP 经原路由可用。
5. 非 Echo 的 Timestamp Request 也经 TCP 到服务器，由服务器出接口发出；目标 INPUT 规则记录来自服务器的一个报文。
6. 将服务器出口 MTU 设为 1280，1300 字节 payload 的 DF Ping 收到 fragmentation-needed / MTU=1280。
7. 重复应用 firewall hook 后 Ping 正常。
8. 停止服务端后 Ping 超时；错 PSK 认证失败并超时；恢复正确配置后自动重连。
9. 删除策略规则后 Ping 超时，独立 FORWARD guard 生效，真实 WAN ICMP 计数为零。
10. SIGKILL daemon 后 Ping 超时，supervisor 自动拉起并恢复。
11. disable 后自有 IPv4/IPv6 链、策略规则、表中路由及 TUN 全部移除。

所有 namespace、测试进程和临时目录由 finally 清理。启动检查等待 TCP 认证成功及 TUN 默认路由就绪；两者之间的启动间隔允许丢包。

## ARMv7 / ARMv8 交付二进制

两种静态二进制均验证 ELF 目标架构和 `version` 命令，并用 QEMU 执行上面的完整网络测试：

```sh
IHT_TEST_ROUTER_BINARY=router/bin/icmptunnel-armv7 \
IHT_TEST_EMULATOR=/usr/bin/qemu-arm \
IHT_TEST_ROUTING_BINARY=bin/linux-amd64/icmptunnel python3 tests/integration.py

IHT_TEST_ROUTER_BINARY=router/bin/icmptunnel-armv8 \
IHT_TEST_EMULATOR=/usr/bin/qemu-aarch64 \
IHT_TEST_ROUTING_BINARY=bin/linux-amd64/icmptunnel python3 tests/integration.py
```

两者全部通过，四跳结果一致，第二至四跳往返约 4 ms（含模拟开销）。这验证了 ARM 构建与 TUN/网络系统调用路径，但不能替代华硕固件的内核模块、软件中心 API、页面资源及开机事件的真机测试。

## 真实 systemd 安装与卸载

在确认宿主不存在同名服务、程序和配置后，用真实 Linux amd64 二进制验证：

- 首次安装后服务 active，TCP listener 可连接。
- 配置 0600、目录 0700、程序 0755。
- 重复安装保留完整配置；显式修改端口/超时保留密钥与公网 IP。
- 提供大写 hex 密钥正确归一化。
- 端口已被占用时安装返回失败，能够检查到 daemon 启动失败。
- 卸载和重复卸载均成功；最终服务 LoadState=not-found，自有程序、配置和 unit 不存在。

未修改宿主防火墙。校验生成/替换密钥只记录 SHA-256，没有把测试密钥写入交付文档。

## 实机交接

请首先在路由器确认 TUN 和 iptables 模块可用，然后按根 README 安装。建议提交测试结果时提供机型全名、固件版本、`uname -m`、`uname -r`、`status` 与 `daemon.log`；隐藏你的 PSK。软件中心网页和固件 hooks 是当前未在真机验证的部分。
