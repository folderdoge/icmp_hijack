<!DOCTYPE html>
<html><head>
<meta charset="utf-8">
<meta http-equiv="X-UA-Compatible" content="IE=edge">
<meta http-equiv="Cache-Control" content="no-store">
<title>ICMP TCP 隧道</title>
<link rel="stylesheet" href="/index_style.css">
<link rel="stylesheet" href="/form_style.css">
<link rel="stylesheet" href="/res/softcenter.css">
<script src="/js/jquery.js"></script>
<script src="/state.js"></script>
<script src="/general.js"></script>
<script src="/popup.js"></script>
<script src="/res/softcenter.js"></script>
<style>
.icmp-panel { max-width:760px; padding:24px; background:#34434c; color:#eee; line-height:1.65; }
.icmp-panel h1 { font-size:23px; margin:0 0 16px; }
.icmp-panel label { display:block; margin:14px 0; }
.icmp-panel input[type=text],.icmp-panel input[type=password],.icmp-panel input[type=number] { width:340px; padding:7px; }
.icmp-panel button { padding:8px 22px; cursor:pointer; }
.icmp-note { color:#becbd2; }
#status { white-space:pre-wrap; margin-top:16px; }
</style>
<script>
function menu_hook() {
    tabtitle[tabtitle.length - 1] = ["", "ICMP TCP 隧道"];
    tablink[tablink.length - 1] = ["", "Module_icmp_hijack.asp"];
}
function statusText(text) { document.getElementById('status').textContent = text; }
function refreshStatus() {
    $.ajax({url:'/_api/icmp_hijack_status', dataType:'json', cache:false,
        success:function(data) { statusText((data.result[0] || {}).icmp_hijack_status || '尚未开启'); },
        error:function() { statusText('读取状态失败，请通过 SSH 执行 status。'); }
    });
}
function init() {
    show_menu(menu_hook);
    $.ajax({url:'/_api/icmp_hijack_', dataType:'json', cache:false,
        success:function(data) {
            var config = data.result[0] || {};
            ['server','port','key'].forEach(function(name) {
                document.getElementById(name).value = config['icmp_hijack_' + name] || (name === 'port' ? '39070' : '');
            });
            document.getElementById('enable').checked = config.icmp_hijack_enable === '1';
            refreshStatus();
        }, error:function() { statusText('读取配置失败，请刷新页面。'); }
    });
}
function save() {
    var active = document.getElementById('enable').checked;
    var server = document.getElementById('server').value.trim();
    var port = document.getElementById('port').value.trim();
    var key = document.getElementById('key').value.trim();
    if (active) {
        var octets = server.split('.');
        if (octets.length !== 4 || octets.some(function(o) { return !/^\d{1,3}$/.test(o) || Number(o) > 255; })) {
            statusText('请输入有效的服务器 IPv4 地址。'); return;
        }
        if (!/^\d{1,5}$/.test(port) || Number(port) < 1 || Number(port) > 65535) {
            statusText('端口范围为 1-65535。'); return;
        }
        if (!/^[a-fA-F0-9]{64}$/.test(key)) {
            statusText('密钥需要服务器安装脚本生成的 64 位十六进制字符串。'); return;
        }
    }
    var id = Math.floor(Math.random() * 100000000);
    document.getElementById('save').disabled = true;
    statusText('正在应用配置…');
    $.ajax({url:'/_api/', type:'POST', dataType:'json',
        data:JSON.stringify({id:id, method:'icmp_hijack_config.sh', params:['apply'],
            fields:{icmp_hijack_enable:active ? '1' : '0', icmp_hijack_server:server, icmp_hijack_port:port, icmp_hijack_key:key}}),
        success:function() { setTimeout(refreshStatus, 2000); },
        error:function() { statusText('提交失败，请刷新后检查。'); },
        complete:function() { document.getElementById('save').disabled = false; }
    });
}
</script>
</head><body onload="init()">
<div id="TopBanner"></div><div id="Loading" class="popup_bg"></div>
<table class="content" style="margin:auto"><tr>
<td style="width:202px;vertical-align:top"><div id="mainMenu"></div><div id="subMenu"></div></td>
<td style="vertical-align:top"><div id="tabMenu" class="submenuBlock"></div>
<div class="icmp-panel">
<h1>ICMP TCP 隧道</h1>
<p>LAN 设备的 IPv4 ICMP 经 TCP 隧道在服务器落地；电脑不需要安装软件。</p>
<label><input type="checkbox" id="enable"> 开启 ICMP 劫持</label>
<label>服务器 IPv4<br><input type="text" id="server" maxlength="15" placeholder="14.137.20.5" autocomplete="off"></label>
<label>TCP 端口<br><input type="number" id="port" min="1" max="65535" value="39070"></label>
<label>共享密钥（64 位 hex）<br><input type="password" id="key" maxlength="64" autocomplete="new-password"></label>
<button id="save" type="button" onclick="save()">保存并应用</button>
<button type="button" onclick="refreshStatus()">刷新状态</button>
<div id="status"></div>
<p class="icmp-note">开启时，远端失联会直接丢弃 ICMP。IPv6 的转发 ICMP 会被丢弃；路由器本机和同一二层 LAN 内通信维持正常。观察逐跳路径请使用 Windows tracert -d -4 或 Linux traceroute -I -n。</p>
<p class="icmp-note">详细连接日志：SSH 执行 /koolshare/icmp_hijack/icmp_hijack.sh status；卸载请在软件中心操作。</p>
<a href="/Module_Softcenter.asp">返回软件中心</a>
</div></td></tr></table><div id="footer"></div>
</body></html>
