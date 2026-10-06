# Linux 服务端

支持带 systemd 的 Debian / Ubuntu，架构为 x86_64 或 aarch64。不需要 Go、Python 或 Docker。

## 从私有 GitHub 仓库一键安装

仓库公开后，可使用下面的免 Token 命令。把 `14.137.20.5` 改成服务器公网 IPv4：

```bash
sudo bash -c 'set -euo pipefail; apt-get update; apt-get install -y curl jq; script=$(mktemp /tmp/icmptunnel-bootstrap.XXXXXX); trap "rm -f -- \"$script\"" EXIT; curl -fsSL --proto "=https" --proto-redir "=https" https://raw.githubusercontent.com/folderdoge/icmp_hijack/main/server/bootstrap.sh -o "$script"; env -u GH_TOKEN -u GITHUB_TOKEN bash "$script" --public-ip 14.137.20.5 --port 39070'
```

脚本支持公开仓库匿名下载，也保留以下私有仓库的认证方式。

下面是一个完整命令，先安装 `curl`、`jq`，再从 GitHub 下载安装脚本、最新 Release 的服务端包及 `SHA256SUMS`，校验后安装并立即启动。把 `14.137.20.5` 改成服务器的公网 IPv4；密钥不填写时自动生成。

```bash
sudo bash -c 'set -euo pipefail; apt-get update; apt-get install -y curl jq; read -rsp "GitHub token: " GH_TOKEN < /dev/tty; printf "\n"; export GH_TOKEN; script=$(mktemp /tmp/icmptunnel-bootstrap.XXXXXX); trap "rm -f -- \"$script\"" EXIT; printf "Authorization: Bearer %s\n" "$GH_TOKEN" | curl -fsSL --proto "=https" --proto-redir "=https" -H @- -H "Accept: application/vnd.github.raw+json" -H "X-GitHub-Api-Version: 2022-11-28" "https://api.github.com/repos/folderdoge/icmp_hijack/contents/server/bootstrap.sh?ref=main" -o "$script"; bash "$script" --public-ip 14.137.20.5 --port 39070'
```

输入 GitHub Token 时不会回显，也不会写入安装配置。为这个私有仓库创建一个 fine-grained personal access token，仅选择 `folderdoge/icmp_hijack`，仓库权限 `Contents: Read-only` 即可。[GitHub Release 下载权限说明](https://docs.github.com/en/rest/releases/assets#get-a-release-asset)。仓库内容和发行包通过 HTTPS 下载；ICMP 隧道使用自己的加密 TCP 协议。

默认下载最新正式 Release。如需固定版本，在最后的 `bash "$script"` 后增加 `--version v1.0.2`。也可先保存 `server/bootstrap.sh`，以 root 执行 `GH_TOKEN` 或 `GITHUB_TOKEN` 已设置的脚本；例如：

```bash
sudo -E bash server/bootstrap.sh --version v1.0.2 --public-ip 14.137.20.5 --port 39070
```

## 从本地发行包安装

下载并完整解压 `icmptunnel-server-版本.tar.gz`，进入 `icmptunnel-server` 目录，再运行：

```bash
sudo bash server/install.sh --public-ip 14.137.20.5 --port 39070
```

脚本会生成 32 字节随机共享密钥，并输出路由器需要填写的 IP、TCP 端口、密钥。如果已有密钥，可以追加 `--key <64位十六进制密钥>`。`--public-ip` 必须填写电脑应看到的第二跳地址；服务端经过 NAT 时填公网地址。

请在云安全组和服务器防火墙中允许指定 TCP 端口，并允许服务器向目标发送 IPv4 ICMP 和接收 ICMP 回应。脚本不修改防火墙。服务器无需开启 IP 转发，无需 TUN 设备；程序通过原始 ICMP socket 发出探测，systemd 服务因此以 root 运行。

重复执行安装命令可更新程序。未指定的配置保留原值；首次安装必须指定 `--public-ip`。默认探测超时是 10 秒，可用 `--timeout 1..60` 调整。GitHub 下载脚本的临时文件和发行包会在安装完成或失败时自动清理。

## 开机自启和管理

安装脚本执行 `systemctl enable --now icmptunnel-server.service`，因此安装后立即启动，也已经启用开机自启。服务进程异常退出时，systemd 会在 2 秒后重启。

安装位置：

- 程序：`/usr/local/bin/icmptunnel`
- 配置：`/etc/icmptunnel/server.json`，权限 600
- 服务：`/etc/systemd/system/icmptunnel-server.service`
- 卸载脚本：`/usr/local/lib/icmptunnel/uninstall.sh`

查看状态和日志：

```bash
systemctl status icmptunnel-server
systemctl is-enabled icmptunnel-server
journalctl -u icmptunnel-server -f
```

修改配置后执行 `sudo systemctl restart icmptunnel-server`。更换密钥时同时更新路由器配置。

卸载：

```bash
sudo bash /usr/local/lib/icmptunnel/uninstall.sh
```

本地完整发行包中的 `sudo bash server/uninstall.sh` 也可使用。卸载停止并取消开机启动，删除上述程序、配置、服务单元和保存的卸载脚本。若自行在 `/etc/icmptunnel` 增加了其他文件，保留这些文件。手动解压的发行包可以自行删除。系统共享的 journal 历史由 systemd 自己管理。
