# 平台与网络行为核对

核对日期：2026-10-06。此文档记录实现依赖的公开原始资料，不代表已经在真实路由器上验证。

## 华硕梅林

- 软件中心的 [离线安装入口](https://github.com/koolshare/rogsoft/blob/ed42d0e9020c83b1872962c42e60bc8a0e68a675/softcenter/softcenter/scripts/ks_tar_install.sh#L205-L222) 在执行插件安装脚本前，检查与 `install.sh` 同级的 `.valid` 包含 `hnd`。官方插件的 [标识文件](https://github.com/koolshare/rogsoft/blob/master/aliddns/aliddns/.valid) 内容为 `hnd`；这与 CPU 的 32/64 位选择分开，组合 ARMv7/ARMv8 包使用相同的 HND 平台声明。

- RMerl 官方 [README 支持列表](https://github.com/RMerl/asuswrt-merlin.ng/blob/main/README-merlin.txt) 将 RT-AX86U / RT-AX86S 列在 3004.388.x 平台。
- “AX5400”不是唯一型号。GNUton 官方仓库 [README](https://github.com/gnuton/asuswrt-merlin.ng/blob/master/README.md) 列有 RT-AX5400、TUF-AX5400 v1 和 DSL-AX5400。安装器应检测实际 `uname -m`，不能仅凭市场名称选择二进制。提供 `linux/arm`、`GOARM=7` 和 `linux/arm64`、`GOARM64=v8.0` 两类静态程序。
- 梅林官方 [User scripts](https://github.com/RMerl/asuswrt-merlin.ng/wiki/User-scripts) 文档说明 `services-start` 在其他系统服务启动后执行，`firewall-start` 在防火墙重建后执行。安装器可以使用这些标准钩子，并在卸载时只移除自己标记的段落。
- Go 官方 [最低要求](https://go.dev/wiki/MinimumRequirements) 说明 Go 1.24 及以后的 Linux 程序要求内核至少 3.2；ARMv7 构建需要 VFPv3。CGO 关闭可避免固件 libc 差异，实际固件可通过 `uname -r` 核对。
- AX86U 388 的 [内核 Makefile](https://github.com/RMerl/asuswrt-merlin.ng/blob/3004.388/release/src-rt-5.02L.07p2axhnd/kernel/linux-4.1/Makefile) 为 4.1.52；GNUton 的 [构建矩阵](https://github.com/gnuton/asuswrt-merlin.ng/blob/master/.github/workflows/github-actions.yml) 区分 TUF-AX5400 的 4.1 构建树与 RT-AX5400 的 4.19 构建树，均满足上述最低要求。此处核对的是源码平台，用户实际固件仍需确认。
- AX86U 388 的 Broadcom [blog.c](https://github.com/RMerl/asuswrt-merlin.ng/blob/3004.388/release/src-rt-5.02L.07p2axhnd/kernel/linux-4.1/net/core/blog.c) IPv4 tuple logger 仅接受 TCP/UDP，其他协议走 skip，因此这条 Flow Cache 建流路径不会缓存 ICMP。未将此结论泛化为所有 4.19 固件，也未自动修改全局硬件加速配置；实机仍应验收断线时的 WAN 泄漏行为。

## TTL、TUN 和 ICMP

- [RFC 1812 §5.3.1](https://www.rfc-editor.org/rfc/rfc1812.html#section-5.3.1) 要求每次转发至少将 TTL 减一；TTL 到期必须丢弃并按规则产生 Time Exceeded。
- Linux [IPv4 转发源码](https://github.com/torvalds/linux/blob/master/net/ipv4/ip_forward.c) 对 TTL 不大于一的转发包产生 Time Exceeded，否则在正常转发中调用 `ip_decrease_ttl`。因此 LAN → TUN 的内核转发自然保留路由器第一跳。
- Linux [TUN/TAP 文档](https://www.kernel.org/doc/html/latest/networking/tuntap.html) 说明 TUN 在用户态传递三层 IP 包，关闭非持久设备的文件描述符后设备及关联路由会消失。程序退出后必须仍有独立 DROP / blackhole 路由保障不能退回普通 WAN 路由。
- 服务端 raw send 属于本机产生的包，不经过 IPv4 forwarding 的 TTL 扣减；服务端须显式再扣一次 TTL。进入服务端时 TTL 为一，则返回以配置的落地 IPv4 为源的 Time Exceeded，使客户端显示第二跳；其后发送 TTL 减一的真实 Echo Request。
- [RFC 792](https://www.rfc-editor.org/rfc/rfc792.html) 定义 Echo ID / Sequence 和 ICMP 错误引用的原始 IPv4 头与至少八字节数据。响应映射需要还原 LAN 源地址、Echo ID 与错误引用。引用可能截断，不能对截断正文重新计算被引用 Echo 的 checksum；可从保存的完整原始 probe 恢复 IPv4 header + ICMP 八字节，或使用 [RFC 1624](https://www.rfc-editor.org/rfc/rfc1624.html) 的增量算法。
- [RFC 4884](https://www.rfc-editor.org/rfc/rfc4884.html) 扩展 ICMP 错误可能带 MPLS 等元数据。恢复原始引用时只替换被改动的 IPv4 header + ICMP 八字节，可保留后续 payload、padding 和扩展。若复制更长引用，应读取 ICMP 第六字节的四字节单位长度，并考虑旧实现长度为零但在 128 字节后携带扩展。
- Linux [IP sysctl 文档](https://docs.kernel.org/networking/ip-sysctl.html) 说明 reverse-path filtering 采用 `all` 与接口配置的最大值。TUN 返回包的源是远端路径 IP，应对新建 TUN 设置 `rp_filter=2`（loose），避免 strict 模式因到远端的反向路由在 WAN 而丢包，同时不必修改全局 sysctl。
- Linux [fib_validate_source 源码](https://github.com/torvalds/linux/blob/v6.6/net/ipv4/fib_frontend.c) 中未编号接口 `no_addr` 会进入 last_resort，启用任意 rp_filter 时被拒绝。因此仅设置 loose 还不够，实际测试后给 TUN 配置了内部 `198.18.0.1/32`；接口关闭时地址自动清理。
- Linux [raw(7)](https://man7.org/linux/man-pages/man7/raw.7.html) 说明创建 raw socket 要求 root / CAP_NET_RAW，接收始终包含 IP 头，`IP_HDRINCL` 发送会由内核填写 IP checksum / total length；使用 `IP_HDRINCL` 的包不会自动分片，并受出口 MTU 限制。若大包返回 `EMSGSIZE`，实现需要报告正确 PMTU 或明确其大包限制，不能假设 Linux 自动分片。

## 自定义加密协议

使用密码库提供的 AES-256-GCM 与 HMAC-SHA256，避免自行实现加密原语。[NIST SP 800-38D](https://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf) 要求相同密钥下的 GCM IV 保持唯一；[RFC 5869](https://www.rfc-editor.org/rfc/rfc5869.html) 提供 HKDF key derivation。两个方向应派生不同密钥，每个会话使用双方随机 nonce，帧使用严格递增计数。双方身份认证应覆盖协议版本、角色与完整握手 nonce。随机强 PSK 不应替换为短口令。

PSK 方案不具备前向保密；TCP 重传与有序交付会影响 ICMP 丢包、抖动和 RTT。此工具显示的是 TCP 隧道往返延迟加服务端实际 ICMP 路径，其本地 WAN 中间跳被隧道隐藏，不能作为未经封装的物理 ICMP 网络质量测量。

## 必须覆盖的交付测试

1. TTL：客户端原始 TTL 一、二、三，分别定位本地网关、落地 IPv4、落地真实下一跳。
2. 两个 LAN IP 使用相同 Echo ID 与 Sequence 并发，响应应准确还原各自来源；不同 TCP 会话也应隔离。
3. 标准 Echo Reply、Time Exceeded、Destination Unreachable（含 PMTU）、带 IPv4 options 的错误引用、短引用和损坏 checksum。
4. 断开服务器、错误密钥、终止客户端、服务重启和防火墙重建期间，WAN 不得出现被劫持的原始 ICMP。
5. 安装后重启仍生效；卸载后本项目服务、钩子片段、iptables 链、策略路由、配置、二进制和自建文件均清理，其他项目的钩子内容保留。
6. IPv6 与 UDP / TCP traceroute 不属于 IPv4 ICMP Echo 转发；支持范围应在使用说明里明确，避免用 Linux 默认 UDP traceroute 判断此功能。
