# 梅林 ICMP TCP 隧道 · 1.0.4

路由器接管 LAN 经路由转发的 IPv4 ICMP，用自定义加密 TCP 协议送到 Linux 服务器。服务器真实发送 ICMP Echo 探测，并将 Echo Reply、Time Exceeded、Destination Unreachable、Parameter Problem 还原给客户端。电脑无需安装软件。

1.0.4 修复梅林 BusyBox 缺少 `command` 内建命令时的依赖误判，包括 iptables、软件中心识别与 IPv6 分支。已用关闭该内建命令的 BusyBox 1.25.1 验证。此前 `id`、外部 `ip` 和 HND `.valid` 的修复继续保留；已有服务端与新版插件协议兼容。

你描述的逐跳显示使用 **Windows `tracert -d`** 或 **Linux `traceroute -n -I`**；普通 `ping` 只显示目标的往返时间。

## 直接使用交付包

安装包在 [GitHub Releases](https://github.com/folderdoge/icmp_hijack/releases/latest) 发布；本地构建输出在 `dist/`。均提供静态二进制，安装时无需 Go、Entware、Python、OpenSSL 或编译器：

| 文件 | 用途 |
| --- | --- |
| [icmp_hijack-1.0.4.tar.gz](https://github.com/folderdoge/icmp_hijack/releases/download/v1.0.4/icmp_hijack-1.0.4.tar.gz) | 路由器插件，内含 ARMv7 / ARMv8，自动选择 |
| [icmptunnel-server-1.0.4.tar.gz](https://github.com/folderdoge/icmp_hijack/releases/download/v1.0.4/icmptunnel-server-1.0.4.tar.gz) | Linux 服务端，内含 x86_64 / aarch64，一键安装 |
| [icmptunnel-source-1.0.4.tar.gz](https://github.com/folderdoge/icmp_hijack/releases/download/v1.0.4/icmptunnel-source-1.0.4.tar.gz) | 完整源码、测试与构建工具 |
| [SHA256SUMS](https://github.com/folderdoge/icmp_hijack/releases/download/v1.0.4/SHA256SUMS) | 三个交付包的 SHA-256 |

### 1. 安装服务端

私有 GitHub 仓库的下载需要只读 Token。可直接运行 [服务端一键命令](server/README.md#从私有-github-仓库一键安装)，它会下载并校验最新 Release，安装后立即启动、启用开机自启，并保存可用的卸载脚本。已有本地发行包时使用下方命令。

支持使用 systemd 的 Debian / Ubuntu 等 Linux。把服务端压缩包上传到服务器，在上传目录运行：

```sh
tar -xzf icmptunnel-server-1.0.4.tar.gz && sudo bash icmptunnel-server/server/install.sh --public-ip 14.137.20.5 --port 39070
```

替换成你的服务器公网 IPv4。脚本自动生成 32 字节随机密钥，并显示 **IP、TCP 端口、64 位 hex 密钥**；将这三项填入路由器。也可通过 `--key` 指定已有密钥。再次执行脚本会升级程序，保留未显式修改的配置。

在云安全组和服务器防火墙放行 **TCP/39070**。服务器要能发出 ICMP 并收到 Echo Reply / Time Exceeded 等回包；本程序不自动改动服务器防火墙。服务器公网 IP 为云平台一对一 NAT 地址时，`--public-ip` 仍填公网地址，发送探测使用内核选择的网卡地址。

```sh
systemctl status icmptunnel-server
journalctl -u icmptunnel-server -f
```

### 2. 安装路由器插件

适配 ARMv7 和 ARMv8 的梅林体系；AX86U 的 388 分支在目标范围。AX5400 的 RT / TUF 等型号不能仅按商品名确定架构，安装器按 `uname -m` 选择。ARMv7 静态程序采用 `GOARM=7`，ARMv8 采用 `GOARM64=v8.0`。

先在路由器「系统管理 → 系统设置」启用 **JFFS 自定义脚本**。有 Koolshare 软件中心时：

1. 打开软件中心的离线安装。
2. 上传 `icmp_hijack-1.0.4.tar.gz` 并安装。
3. 打开「ICMP TCP 隧道」，填入 IP / 端口 / 密钥，开启并应用。

无软件中心的原生梅林：把同一个包上传到路由器 `/tmp`，通过 SSH 执行：

```sh
cd /tmp
tar -xzf icmp_hijack-1.0.4.tar.gz
sh /tmp/icmp_hijack/install.sh
vi /jffs/icmp_hijack/config.json
```

配置示例，密钥必须换成服务端显示的值：

```json
{
  "server": "14.137.20.5",
  "port": 39070,
  "key": "服务端生成的64位十六进制密钥",
  "interface": "icmptun0"
}
```

```sh
/jffs/icmp_hijack/icmp_hijack.sh enable
```

配置修改后执行 `restart`。安装本身默认关闭；启用后连接失败、认证失败或隧道进程退出时，接管范围内的 ICMP 丢弃，不走真实 WAN。

详见 [路由器安装、日志和卸载说明](router/README.md)；[服务端说明](server/README.md)。

### 3. 在电脑上验证

Windows：

```bat
ping -4 123.100.97.2
tracert -4 -d 123.100.97.2
```

Linux：

```sh
ping -4 123.100.97.2
traceroute -4 -n -I 123.100.97.2
```

预期路径：第一跳 `192.168.50.1`，第二跳 `14.137.20.5`，第三跳起是服务器侧的实际路由，最后是目标地址。网关是否回应 ICMP 由远端网络决定，不回应的跳点仍显示 `*`。第二跳是服务端收到 TTL 到期探测后生成的回应，后续地址来自实际网络回包。

RTT 是 **电脑 ↔ 路由器 ↔ TCP 落地服务器 ↔ 探测节点** 的实际往返时间，没有伪造延迟；相对服务器本地执行 traceroute，它包含到服务器的隧道开销。TCP 重传和排队也会影响 ICMP 的丢包、RTT 和抖动表现。

### 4. 故障验证

在服务器执行 `sudo systemctl stop icmptunnel-server`，电脑再次 Ping 公网目标，应超时。电脑 Ping 路由器 LAN 地址仍正常。随后 `sudo systemctl start icmptunnel-server`，路由器会自动重连。

路由器查看状态（软件中心版）：

```sh
/koolshare/icmp_hijack/icmp_hijack.sh status
tail -n 50 /tmp/icmp_hijack/daemon.log
```

原生版把第一条路径替换为 `/jffs/icmp_hijack/icmp_hijack.sh`。`tunnel connected` 是 TCP 与密钥认证成功，`handshake failed` 通常是密钥不一致。软件中心状态栏显示应用结果，实际连接情况以日志为准。

### 5. 卸载

软件中心直接卸载，或路由器 SSH 执行：

```sh
/koolshare/icmp_hijack/uninstall.sh
# 原生梅林：/jffs/icmp_hijack/uninstall.sh
```

服务端执行：

```sh
sudo bash /usr/local/lib/icmptunnel/uninstall.sh
```

路由器卸载清理自有文件、dbus 项、页面、图标、临时日志、进程、链、策略规则、路由，并恢复原有 hooks 的内容和权限。服务端卸载清理自有服务单元、程序和配置。你手动上传的压缩包、解压目录和手动开放的云安全组端口由你管理。

## 支持范围和边界

- **接管范围：** 从 `br+` 网桥进入并由路由器转发的 IPv4 ICMP。回应路由器自身的 Ping、同一二层网段内直接交换的 Ping 不经过本隧道。其他插件若把客户端放在非 `br` 开头的接口，需调整脚本中的入口匹配。
- **实际转发：** 完整的 IPv4 ICMP 数据报均经 TCP 投递并从服务端发出；Echo Request 建立回包映射，支持 Echo Reply 和关联 ICMP 错误。其他 ICMP 类型保持正文直接发送，不建立其回应映射；IPv4 分片被丢弃。Linux 默认 UDP traceroute、TCP traceroute 和实际业务 TCP/UDP 走原链路。这里只改变 ICMP 探测视角。
- **IPv6：** 暂无 ICMPv6 隧道；开启期间阻断由 LAN 网桥转发的 ICMPv6，避免 IPv6 Ping 绕过。路由器本地 NDP 不受此规则影响，但 IPv6 端到端诊断和部分 PMTU 场景会受影响。测试请使用上述 IPv4 命令。
- **包长：** TUN MTU 为 1500，常规 Ping/tracert 适用。IPv4 分片不转发；服务端 raw socket 超出出口 MTU 时回应 ICMP fragmentation-needed，并带内核报告的 MTU，不额外实现非 DF 分片。
- **路由器内核：** 需要 TUN、iptables raw/NOTRACK、mangle MARK、addrtype 和策略路由。缺少这些能力会报错并保持丢包，日志给出定位信息。TUN 使用 `198.18.0.1/32` 作为内部地址，启用期间该地址属于路由器，不作为远端探测目标；TUN 关闭后自动移除。
- **配置资源：** 自有表 `18888`、规则优先级 `100`、mark 位 `0x40000000`。优先级被其他功能占用时不会删除对方规则，插件报错并保持丢包。其他插件若更早处理流量可能导致探测无法成功；正常运行和自身重载的断线保护已经验证。固件/其他程序全量清空规则后、重建回调运行前的窗口不属于本插件能保证的范围。
- **密钥协议：** 随机 256 位 PSK、双向随机挑战与 HMAC-SHA256 认证、独立方向 AES-256-GCM 会话密钥、递增 nonce 和长度认证。不是 HTTP，也不是明文口令或 XOR；PSK 方案没有前向保密。协议详见 [设计说明](docs/protocol.md)。

## 已完成的验证

没有连接华硕路由器实机，软件中心页面尚需你在实际固件上安装验证。已完成：

- 五个 Linux network namespace 的真实 TUN / iptables / TCP / raw ICMP 测试，使用实际路由器控制脚本：逐跳路径、普通 Ping、两个 LAN 地址使用相同 ID/sequence、重载、断线和重连。
- 服务端断开、策略规则被移除、daemon 被杀时无直接 WAN ICMP 泄漏；自动拉起后恢复；关闭后自有链、规则、路由、TUN 清理。
- 真实 systemd 安装、启动、保留配置升级、修改配置、端口占用失败、卸载和二次卸载。
- 两种路由器安装模式的隔离模拟：hooks 内容/权限恢复、外部规则保留、配置验证、安装卸载文件及 dbus 清理。
- Go 单元测试与 vet：认证、加密帧篡改/重放、并发发送、身份映射、校验和、ICMP 截断引用、IP options、RFC4884 扩展。
- Go race 检查通过；内置路由操作通过真实 namespace 测试，并与 iproute2 核对，验证精确删除不会删掉其他 mark/mask/table/metric 的规则和路由。
- ARMv7/ARMv8 的协议与数据路径通过模拟测试；QEMU user 8.2 不支持策略规则消息，因此 1.0.3 模拟器测试使用原生内置路由助手，另在两种 ARM 下验证请求编码。实际 ARM 固件的路由系统调用仍需实机验证。

详细记录见 [测试记录](docs/verification.md)。

## 从源码构建

源码无第三方 Go 依赖。使用 Go 1.24+ 和 Python 3（构建本次交付的工具链为 Go 1.27.1）：

```sh
go test ./...
go vet ./...
python3 tools/release.py --version 1.0.4
```

Linux 完整网络测试需要 root，以及 `ip`、`iptables`、`ip6tables`、`ping`、`traceroute`：

```sh
sudo python3 tests/integration.py
python3 router/tests/verify_scripts.py
python3 router/tests/verify_command_lookup.py
python3 router/tests/verify_offline_package.py
sudo python3 server/tests/test_install.py
python3 server/tests/test_bootstrap.py
```

相关平台、内核、ICMP 引用和 TUN 的原始依据见 [资料核对](docs/research.md)。
