# 实现与协议

## 数据路径和 TTL

1. 路由器 raw PREROUTING 对 `br+` 进入、目的不是路由器本地地址的 IPv4 ICMP 使用 NOTRACK，避免旧连接 NAT 修改客户端源地址或 echo ID。
2. mangle 写入 `0x40000000`，策略规则将其送入表 `18888`。该表同时有指向 TUN 的默认路由和低优先级 blackhole 默认路由。
3. 路由器内核正常消耗一跳 TTL。TTL=1 的探测由路由器返回 Time Exceeded，形成第一跳；TTL>=2 的包由 TUN 交给程序，此时 TTL 已减一。
4. 程序通过唯一的 TCP 连接发送完整的 IPv4 ICMP 数据报。断线时直接丢弃；队列中的包绑定其原会话，不在新会话补发旧探测。
5. 服务端把自己作为第二跳：接到 TTL=1 且目标不是自身公网地址的包，生成源地址为 `public_ip` 的 Time Exceeded。其余探测再减一 TTL 后通过 `IPPROTO_RAW/IP_HDRINCL` 发出。目标为 `public_ip` 时直接返回 Echo Reply，允许在第二跳结束。
6. 出站源 IP 为零，让 Linux 填入出接口源地址；每个探测使用一个全局唯一的在途 echo ID，保存完整原始探测和所属会话。
7. ICMP raw receive 读取真实网络回应。Echo Reply 匹配 ID / sequence / 目标 / payload；ICMP 错误从引用中匹配 ID / sequence / 目标。过期、坏 checksum、无匹配的回包忽略。
8. Echo Reply 恢复客户端目标地址与原始 echo ID；ICMP 错误恢复被引用的原始 IPv4 头及 ICMP 八字节，保留其余正文和 RFC4884 扩展。重算外层校验和，交回路由器 TUN。回包 TTL 最小为 2，允许它再经路由器转发一跳。

非 Echo ICMP 也通过 TCP 送到服务器，扣 TTL 并以服务器出接口地址发送，保留 ICMP 正文，不建立返回映射。TTL 到期的非 Echo 包直接丢弃，避免对 ICMP 错误再生成错误。完整双向诊断范围是 Echo 和关联错误；分片不支持。

服务器探测源地址是服务器地址，不是客户端的私网地址。服务器无需 TUN、SNAT iptables 或开启 IP forwarding。路由器有独立 FORWARD 丢包规则，缺少 mark 或策略规则时也不允许 LAN ICMP 走 WAN。

TUN 非持久，关闭 fd 后内核移除设备及其地址/设备路由；独立 blackhole 不依赖设备存在。接口设置 `rp_filter=2`，并配置内部 `198.18.0.1/32`，避免全局 strict rp_filter 和未编号接口检查丢弃回包；不修改全局 sysctl。

1.0.3 起 shell 的 `ip` 调用由同一个静态程序的内部子命令执行，用 IPv4 rtnetlink 实现接口查询、策略规则及默认/blackhole 路由。规则删除同时匹配 pref/mark/mask/table，路由删除匹配 table/type/metric/device；不安装额外程序或改动系统命令链接。

## IHT1 握手

仅在已连接 TCP 流上运行，所有整数使用网络字节序。PSK 是 32 字节随机值，以 64 位 hex 配置。

| 方向 | 字节 |
| --- | --- |
| client → server | `"IHT1"`（4） + client random（32） |
| server → client | server random（32） + server HMAC（32） |
| client → server | client HMAC（32） |

`server HMAC = HMAC-SHA256(PSK, "IHT1/server" || client_random || server_random)`，client 使用 `"IHT1/client"`。随机值来自系统 CSPRNG。每次握手双方均重新取随机值，握手 deadline 为 8 秒。

令 `transcript = client_random || server_random`，用 HMAC-SHA256 HKDF 的 extract/expand 构造：

```text
PRK = HMAC-SHA256(key=transcript, data=PSK)
c2s = HMAC-SHA256(PRK, "IHT1/c2s" || 0x01)
s2c = HMAC-SHA256(PRK, "IHT1/s2c" || 0x01)
```

两方向各自使用 AES-256-GCM，密钥相互独立。本协议适用于已配对的自用服务器，未经过独立密码审计，也不提供 PSK 泄露后的前向保密。

## 加密帧

```text
uint32 encrypted_length || AES-GCM(type_byte || payload)
```

- 四字节长度作为 AAD 认证；明文最大 65536 字节（type + 最大 IPv4 包）。
- 96 位 GCM nonce = 四字节零 + 八字节计数器；两个方向分别从零严格递增。不同方向使用不同密钥，计数器溢出关闭。
- 类型 1 是 IPv4 包；类型 2/3 是空 payload 的 Ping/Pong。路由器每 5 秒发送心跳；读 deadline 为 20 秒，写 deadline 为 5 秒。
- 不携带显式 sequence；接收端按 TCP 顺序推导 nonce，重复、删帧、篡改或长度变化会导致认证失败并关闭连接。
- 路由器连接失败或断开后每 3 秒重试。没有 HTTP、WebSocket、DNS 封装或自动降级链路。

## 文件与进程

路由器一个 Go daemon 加轻量 shell supervisor；服务端一个 Go daemon 加 systemd。探测映射默认保留 10 秒，可接受正常重复回应，随后释放；服务端断开会话时移除所属映射。Go daemon 同时编译两种模式以共用协议和包处理代码，无运行时第三方依赖。
