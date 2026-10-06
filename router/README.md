# 路由器插件安装与测试

插件把经过华硕路由器转发的 LAN IPv4 ICMP 交给 `icmptun0`，通过自定义加密 TCP 协议送到服务器。电脑不用安装软件。安装包同时包含 ARMv7 和 ARMv8 二进制，安装脚本按 `uname -m` 选择，目标平台为 AX5400 与 AX86U，包括 AX86U 的 388 固件。

目前已经在 Linux 网络命名空间里验证实际数据包转发和脚本行为，尚未在华硕真机上运行。真机需要固件提供 TUN、iptables 的 MARK/NOTRACK/addrtype 模块和策略路由内核能力；不需要外部 `ip` 命令。

## 安装前

1. 先部署服务端，记下服务器 IPv4、TCP 端口和生成的 **64 位十六进制密钥**。默认端口是 `39070`。
2. 在路由器「系统管理 → 系统设置」开启 JFFS 自定义脚本；测试时开启 SSH。
3. 确认路由器处于路由模式，电脑的默认网关是这台路由器。

服务器地址填写 IPv4，不填写域名。密钥直接复制服务端输出，端口也必须和服务端一致。

## 有软件中心：离线安装

在软件中心的离线安装页面上传 `icmphijack-1.0.5.tar.gz`，完成安装后打开「ICMP TCP 隧道」。填写 IPv4、端口、密钥，勾选「开启 ICMP 劫持」，点击「保存并应用」。初次安装默认关闭。

软件中心页面地址是 `/Module_icmphijack.asp`。程序安装在 `/koolshare/icmp_hijack/`，生成的配置位于 `/koolshare/configs/icmp_hijack.json`。配置由页面里的 dbus 参数生成，修改配置请使用页面；直接修改 JSON 会在下次应用时被覆盖。

安装包必须使用以下结构，最外层目录名为 `icmphijack`，不能再套一层目录：

```text
icmphijack/
  .valid                    # 内容为 hnd，软件中心安装前检查
  install.sh
  uninstall.sh
  bin/icmptunnel-armv7
  bin/icmptunnel-armv8
  scripts/...
  webs/Module_icmphijack.asp
  res/icon-icmphijack.png
```

1.0.5 的软件中心标识是 `icmphijack`，因为中心按下划线拆注册键，旧的 `icmp_hijack` 会被误解析。配置前缀和运行目录继续保留旧名称，不改动已有配置。

已安装 1.0.4 时，将 `repair_center.sh` 上传为 `/tmp/repair_center.sh`，执行 `sh /tmp/repair_center.sh` 后刷新软件中心即可补回卡片，不需要重装。修复脚本同时补齐正确图标及卸载入口，保留原配置和启用状态。

## 原生梅林，没有软件中心

通过 SCP 或 WinSCP 把同一个安装包上传到路由器的 `/tmp/icmphijack-1.0.5.tar.gz`，然后 SSH 执行：

```sh
cd /tmp
tar -xzf icmphijack-1.0.5.tar.gz
sh /tmp/icmphijack/install.sh
vi /jffs/icmp_hijack/config.json
```

把配置改成自己的服务器参数，例如：

```json
{
  "server": "14.137.20.5",
  "port": 39070,
  "key": "替换为服务器生成的64位十六进制密钥",
  "interface": "icmptun0"
}
```

`interface` 保持 `icmptun0`。完成编辑后开启：

```sh
/jffs/icmp_hijack/icmp_hijack.sh enable
/jffs/icmp_hijack/icmp_hijack.sh status
```

启用状态会保存，重启后自动恢复。以后修改 JSON，执行 `restart` 应用新配置：

```sh
/jffs/icmp_hijack/icmp_hijack.sh restart
```

原生梅林安装方式不添加配置网页。

## 在电脑上测试

普通 `ping` 显示目标是否回应和往返时间；查看逐跳结果要用 ICMP 路由追踪。

Windows：

```powershell
ping -4 123.100.97.2
tracert -d -4 123.100.97.2
```

Linux：

```sh
ping -4 123.100.97.2
traceroute -4 -I -n 123.100.97.2
```

正常路径的第一跳是本地路由器，第二跳是配置的落地服务器，后续跳点来自该服务器到目标的真实路径。每一行时间都包含电脑经路由器到落地服务器的传输耗时；路由器或目标设备不回应 ICMP 时，相应跳点仍会显示 `*`。

测试断线行为：暂时停止服务端或阻断服务器的 TCP 端口，再执行 `ping` 和 `tracert`。目标请求应超时，不能出现本地真实 WAN 的中间跳点。恢复服务端后，隧道会自动重连。

## 查看状态与日志

软件中心安装：

```sh
/koolshare/icmp_hijack/icmp_hijack.sh status
```

原生梅林安装：

```sh
/jffs/icmp_hijack/icmp_hijack.sh status
```

两种方式的运行日志都在：

```sh
tail -n 50 /tmp/icmp_hijack/daemon.log
tail -n 50 /tmp/icmp_hijack/control.log
```

`status` 展示启用状态、进程、策略路由和最近的隧道日志。进程运行不代表服务器已经连接；判断连接、密钥错误和重连原因时，以 `daemon.log` 为准。页面里的状态是最近一次控制操作结果。

发生异常时可以补充检查：

```sh
uname -m
uname -r
ls -l /dev/net/tun
/koolshare/icmp_hijack/bin/icmptunnel ip link show icmptun0
/koolshare/icmp_hijack/bin/icmptunnel ip rule show
/koolshare/icmp_hijack/bin/icmptunnel ip route show table 18888
iptables -t filter -L ICMP_HIJACK -n -v
```

原生梅林把上述 `/koolshare/icmp_hijack` 替换为 `/jffs/icmp_hijack`。程序的 `ip` 子命令仅实现插件使用的 IPv4 查询和策略操作，不是完整 iproute2 替代品。

如果提示 `/dev/net/tun` 不可用，启动脚本已经尝试过 `modprobe tun`。可以通过 SSH 再运行 `modprobe tun` 查看具体错误。若固件缺少匹配的 TUN 内核模块，必须使用提供该模块的固件；创建同名普通文件无法解决。保持启用时，插件继续丢弃被劫持的 ICMP。

## 关闭与卸载

关闭劫持会恢复普通 ICMP 转发。有软件中心时取消勾选并保存，或执行：

```sh
/koolshare/icmp_hijack/icmp_hijack.sh disable
```

原生梅林执行：

```sh
/jffs/icmp_hijack/icmp_hijack.sh disable
```

卸载优先使用软件中心的卸载按钮，也可以执行：

```sh
/koolshare/scripts/uninstall_icmphijack.sh
```

原生梅林卸载：

```sh
/jffs/icmp_hijack/uninstall.sh
```

卸载会删除插件进程、配置、日志、dbus 参数、自己的防火墙链、策略规则及钩子行。已有的用户钩子和其他路由/防火墙规则会保留。手工上传到 `/tmp` 的压缩包和解压目录可在确认安装成功后自行删除；它们不是运行依赖，重启也会清除。

## 实现范围

- 劫持入口为桥接接口 `br+`，覆盖常见的 `br0` 和其他 `br*` LAN 桥。自行创建、名称不以 `br` 开头的 LAN 接口需要修改规则适配。
- 同一二层网络内、直接在交换机上互通的电脑流量不会经过路由器的三层转发，不能用这个插件劫持。发给路由器本机的 ICMP 也不进入隧道；本机第一跳仍然正常。
- 隧道只传 IPv4。启用期间，从 LAN 转发出去的 ICMPv6 被丢弃，避免 IPv6 测试绕过隧道；本机 INPUT/OUTPUT 的 IPv6 邻居发现仍由固件处理。这也会影响需要转发的 ICMPv6 错误/路径 MTU 消息，测试完成后可以关闭插件。
- 隧道接口使用保留的测试地址 `198.18.0.1/32`，这个地址属于路由器本机，不能作为需要劫持的目标。接口关闭时地址一起删除。
- 插件使用 `ICMP_HIJACK` 和临时 `ICMP_HIJACK_GUARD` 链、fwmark `0x40000000/0x40000000`、策略优先级 `100`、路由表 `18888`。优先级被其他功能占用时，会报告冲突并保持丢弃；不要把这些资源交给其他插件共用。
- 捕获的 ICMP 使用 raw 表 `NOTRACK`，避免路由器已有的连接跟踪/NAT 改写 LAN 源地址和 ICMP 标识；返回包也使用 `NOTRACK`。TCP 隧道连接仍使用普通路由和固件 NAT。
- 断线时丢包；隧道进程退出后由监督进程重启。TUN 路由消失时使用黑洞默认路由；独立转发丢弃规则还会阻止策略规则丢失造成的真实 WAN 回落。
- 接收规则不能跨越固件完全清空防火墙到调用自定义钩子之间的所有瞬间；插件会在 `firewall-start` 和 `nat-start` 重新应用规则。普通断线、进程退出以及插件自身重载已按保持丢弃设计。

Linux 网络命名空间集成测试已验证四跳路径、两个客户端使用相同 Echo ID、TCP 重连、删除策略规则时没有 WAN 回落、隧道进程退出后监督重启，以及关闭后的规则清理。脚本模拟测试还验证了两种安装模式、重入、规则安装失败时保留丢弃保护、原有钩子内容和权限保留、配置验证和干净卸载。

固件事件脚本依据 [Asuswrt-Merlin 官方说明](https://github.com/RMerl/asuswrt-merlin.ng/wiki/User-scripts)；软件中心页面与安装结构参考 [Koolshare 软件中心源码](https://github.com/koolshare/rogsoft/blob/master/softcenter/softcenter/scripts/ks_app_install.sh) 和 [其配置页 RPC 用法](https://github.com/koolshare/rogsoft/blob/master/aliddns/aliddns/webs/Module_aliddns.asp)。
