<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta http-equiv="X-UA-Compatible" content="IE=edge">
<meta http-equiv="Cache-Control" content="no-store">
<meta http-equiv="Pragma" content="no-cache">
<title>ICMP TCP 隧道</title>
<link rel="shortcut icon" href="/images/favicon.png">
<link rel="stylesheet" href="/index_style.css">
<link rel="stylesheet" href="/form_style.css">
<link rel="stylesheet" href="/css/element.css">
<link rel="stylesheet" href="/res/softcenter.css">
<script src="/js/jquery.js"></script>
<script src="/state.js"></script>
<script src="/general.js"></script>
<script src="/popup.js"></script>
<script src="/help.js"></script>
<script src="/res/softcenter.js"></script>
<style>
#FormTitle { width:760px; }
.icmp-heading { position:relative; padding:12px 5px 0; }
.icmp-return { position:absolute; right:12px; top:10px; }
.icmp-return img { border:0; }
.icmp-intro { margin:12px 5px; font-size:12px; line-height:1.8; }
.icmp-table { width:100%; margin-top:8px; }
.icmp-table th { width:30%; }
.icmp-table td { line-height:1.8; }
.icmp-version { float:right; margin:5px 16px 0 12px; font-size:12px; }
.icmp-state { display:inline-block; margin-right:10px; font-weight:bold; }
#state.icmp-state-disabled { color:#bec6cb !important; }
#state.icmp-state-connecting { color:#f6d36e !important; }
#state.icmp-state-connected { color:#83dc8f !important; }
#state.icmp-state-error { color:#ff9696 !important; }
#status { overflow-wrap:break-word; color:#e1e8eb !important; }
.icmp-status-meta { color:#bbc5ca; font-size:11px; }
.icmp-tabs { margin-top:14px; border-bottom:1px solid #67767d; }
.icmp-tab { cursor:pointer; padding:9px 16px; font-size:13px; color:#fff; background:#67767d; border:1px solid #67767d; border-bottom:0; border-radius:5px 5px 0 0; margin-right:4px; }
.icmp-tab.active, .icmp-tab:hover { background:#2f3a3e; border-color:#2f3a3e; }
.icmp-config { margin-top:0; }
.icmp-config input.input_ss_table { width:260px; }
.icmp-config input.input_ss_table { background:#586f74; color:#fff; border:1px solid #7b949b; padding:5px 7px; font:13px Tahoma,Arial,sans-serif; box-sizing:border-box; }
#version, #status_time { color:#c5d1d7 !important; }
.icmp-config #port { width:95px; }
.icmp-config #key { width:310px; font-family:monospace; }
.icmp-key-toggle { margin-left:8px; font-size:12px; white-space:nowrap; }
.icmp-hint { color:#c2ccd1; font-size:11px; margin-top:3px; }
.icmp-help { padding:10px 16px; font-size:12px; line-height:2; }
.icmp-help p { margin:8px 0; }
.icmp-help code { color:#fff; }
.icmp-form-message { min-height:20px; margin:10px 5px 0; font-size:12px; line-height:1.7; }
.icmp-form-error { color:#ff9696; }
.icmp-form-notice { color:#d6e0e5; }
#apply_button { padding-top:8px; padding-bottom:18px; }
</style>
<script>
var configurationReady = false;
var statusTimer = null;
var statusRequestPending = false;
var pageActive = true;

function menu_hook() {
    tabtitle[tabtitle.length - 1] = ["", "ICMP TCP 隧道"];
    tablink[tablink.length - 1] = ["", "Module_icmphijack.asp"];
}

function element(id) { return document.getElementById(id); }

function setFormMessage(message, kind) {
    element('form_message').textContent = message;
    element('form_message').className = 'icmp-form-message' + (kind ? ' icmp-form-' + kind : '');
}

function selectTab(tab) {
    var configuring = tab === 'config';
    element('tablet_1').style.display = configuring ? '' : 'none';
    element('tablet_2').style.display = configuring ? 'none' : '';
    element('apply_button').style.display = configuring ? '' : 'none';
    element('show_btn1').className = 'show-btn1 icmp-tab' + (configuring ? ' active' : '');
    element('show_btn2').className = 'show-btn2 icmp-tab' + (configuring ? '' : ' active');
    element('show_btn1').setAttribute('aria-selected', configuring ? 'true' : 'false');
    element('show_btn2').setAttribute('aria-selected', configuring ? 'false' : 'true');
}

function toggleKey() {
    element('key').type = element('show_key').checked ? 'text' : 'password';
}

function formatStatusTime(value) {
    var raw = String(value || '');
    if (!raw) return '—';
    var date = /^\d+$/.test(raw) ? new Date(Number(raw) * (raw.length <= 10 ? 1000 : 1)) : new Date(raw);
    if (isNaN(date.getTime())) return raw;
    function pad(number) { return number < 10 ? '0' + number : String(number); }
    return date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate()) +
        ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes()) + ':' + pad(date.getSeconds());
}

function renderStatus(config) {
    var labels = {
        disabled:['已关闭', 'disabled'], connecting:['正在连接', 'connecting'],
        connected:['已连接', 'connected'], connect_error:['连接失败', 'error'],
        auth_error:['认证失败', 'error'], error:['运行异常', 'error']
    };
    var state = config.icmp_hijack_state || (config.icmp_hijack_enable === '1' ? 'connecting' : 'disabled');
    var presentation = labels[state] || labels.error;
    element('state').textContent = presentation[0];
    element('state').className = 'icmp-state icmp-state-' + presentation[1];
    element('status').textContent = config.icmp_hijack_status ||
        (state === 'disabled' ? 'ICMP 劫持未开启。' : '等待当前状态更新。');
    element('status_time').textContent = formatStatusTime(config.icmp_hijack_status_time);
    if (config.icmp_hijack_version) {
        element('version').textContent = '当前版本：' + config.icmp_hijack_version;
    }
}

function scheduleStatus() {
    if (statusTimer !== null) clearTimeout(statusTimer);
    if (pageActive) statusTimer = setTimeout(refreshStatus, 3000);
}

function refreshStatus() {
    if (!pageActive || statusRequestPending) return;
    statusRequestPending = true;
    $.ajax({url:'/_api/icmp_hijack_', dataType:'json', cache:false, timeout:10000,
        success:function(data) {
            renderStatus((data && data.result && data.result[0]) || {});
        },
        error:function() {
            renderStatus({icmp_hijack_state:'error', icmp_hijack_status:'当前状态读取失败，请检查路由器连接。'});
        },
        complete:function() { statusRequestPending = false; scheduleStatus(); }
    });
}

function stopPolling() {
    pageActive = false;
    if (statusTimer !== null) clearTimeout(statusTimer);
}

function init() {
    show_menu(menu_hook);
    selectTab('config');
    $.ajax({url:'/_api/icmp_hijack_', dataType:'json', cache:false, timeout:10000,
        success:function(data) {
            var config = (data && data.result && data.result[0]) || {};
            var names = ['server', 'port', 'key'];
            for (var i = 0; i < names.length; i++) {
                var name = names[i];
                element(name).value = config['icmp_hijack_' + name] || (name === 'port' ? '39070' : '');
                element(name).readOnly = false;
            }
            element('enable').checked = config.icmp_hijack_enable === '1';
            element('enable').disabled = false;
            element('show_key').disabled = false;
            configurationReady = true;
            element('save').disabled = false;
            renderStatus(config);
        },
        error:function() {
            setFormMessage('读取配置失败，请刷新页面后重试。', 'error');
            renderStatus({icmp_hijack_state:'error', icmp_hijack_status:'当前状态读取失败。'});
        },
        complete:scheduleStatus
    });
}

function save() {
    if (!configurationReady) return;
    var active = element('enable').checked;
    var server = element('server').value.trim();
    var port = element('port').value.trim();
    var key = element('key').value.trim();
    if (active) {
        var octets = server.split('.');
        if (octets.length !== 4 || octets.some(function(o) {
            return !/^(0|[1-9]\d{0,2})$/.test(o) || Number(o) > 255;
        })) {
            setFormMessage('请输入有效的服务器 IPv4 地址。', 'error'); return;
        }
        if (!/^\d{1,5}$/.test(port) || Number(port) < 1 || Number(port) > 65535) {
            setFormMessage('TCP 端口范围为 1–65535。', 'error'); return;
        }
        if (!/^[a-fA-F0-9]{64}$/.test(key)) {
            setFormMessage('共享密钥应为服务端生成的 64 位十六进制字符串。', 'error'); return;
        }
    }
    var id = Math.floor(Math.random() * 100000000);
    element('save').disabled = true;
    setFormMessage('正在提交配置…', 'notice');
    $.ajax({url:'/_api/', type:'POST', dataType:'json', timeout:15000,
        data:JSON.stringify({id:id, method:'icmp_hijack_config.sh', params:['apply'],
            fields:{icmp_hijack_enable:active ? '1' : '0', icmp_hijack_server:server,
                icmp_hijack_port:port, icmp_hijack_key:key}}),
        success:function() {
            setFormMessage('配置已提交，当前状态将自动刷新。', 'notice');
            refreshStatus();
        },
        error:function() { setFormMessage('提交失败，请检查路由器连接后重试。', 'error'); },
        complete:function() { element('save').disabled = false; }
    });
}
</script>
</head>
<body onload="init();" onunload="stopPolling();">
<div id="TopBanner"></div>
<div id="Loading" class="popup_bg"></div>
<table class="content" align="center" cellpadding="0" cellspacing="0">
<tr>
    <td width="17">&nbsp;</td>
    <td width="202" valign="top"><div id="mainMenu"></div><div id="subMenu"></div></td>
    <td valign="top">
        <div id="tabMenu" class="submenuBlock"></div>
        <table width="98%" border="0" align="left" cellpadding="0" cellspacing="0">
        <tr><td valign="top">
            <table id="FormTitle" class="FormTitle" width="760" border="0" cellpadding="5" cellspacing="0" bordercolor="#6b8fa3">
            <tr><td bgcolor="#4D595D" valign="top">
                <div class="icmp-heading">
                    <div class="formfonttitle">ICMP TCP 隧道</div>
                    <a class="icmp-return" href="/Module_Softcenter.asp" title="返回软件中心">
                        <img id="return_btn" src="/images/backprev.png" alt="返回软件中心"
                            onmouseover="this.src='/images/backprevclick.png';" onmouseout="this.src='/images/backprev.png';">
                    </a>
                </div>
                <div class="splitLine" style="margin:12px 0 10px 5px;"></div>
                <div class="icmp-intro">将 LAN 设备的 IPv4 ICMP 通过 TCP 隧道交给远端服务器，电脑无需安装软件。</div>

                <table class="FormTable icmp-table" border="1" cellpadding="4" cellspacing="0" bordercolor="#6b8fa3">
                <thead><tr><td colspan="2">ICMP TCP 隧道 — 开关 / 状态</td></tr></thead>
                <tbody>
                <tr>
                    <th><label for="enable">开启 ICMP 劫持</label></th>
                    <td>
                        <div class="switch_field" style="display:inline-block;vertical-align:middle;">
                            <label for="enable">
                                <input id="enable" class="switch" type="checkbox" disabled="disabled" style="display:none;">
                                <div class="switch_container"><div class="switch_bar"></div><div class="switch_circle transition_style"><div></div></div></div>
                            </label>
                        </div>
                        <span id="version" class="icmp-version">当前版本：—</span>
                    </td>
                </tr>
                <tr>
                    <th>当前状态</th>
                    <td aria-live="polite">
                        <span id="state" class="icmp-state icmp-state-disabled">读取中</span><span id="status">正在获取当前状态。</span>
                        <div class="icmp-status-meta">状态变化时间：<span id="status_time">—</span></div>
                    </td>
                </tr>
                </tbody>
                </table>

                <div class="icmp-tabs" role="tablist" aria-label="插件设置">
                    <input id="show_btn1" class="show-btn1 icmp-tab active" type="button" value="服务配置" role="tab" aria-selected="true" aria-controls="tablet_1" onclick="selectTab('config');">
                    <input id="show_btn2" class="show-btn2 icmp-tab" type="button" value="帮助信息" role="tab" aria-selected="false" aria-controls="tablet_2" onclick="selectTab('help');">
                </div>
                <div id="tablet_1" role="tabpanel" aria-labelledby="show_btn1">
                    <table class="FormTable icmp-table icmp-config" border="1" cellpadding="4" cellspacing="0" bordercolor="#6b8fa3">
                    <tbody>
                    <tr>
                        <th><label for="server">服务器 IPv4</label></th>
                        <td><input id="server" class="input_ss_table" type="text" maxlength="15" placeholder="14.137.20.5" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" readonly="readonly"></td>
                    </tr>
                    <tr>
                        <th><label for="port">TCP 端口</label></th>
                        <td><input id="port" class="input_ss_table" type="text" maxlength="5" inputmode="numeric" value="39070" autocomplete="off" readonly="readonly"><span class="icmp-hint">&nbsp;1–65535</span></td>
                    </tr>
                    <tr>
                        <th><label for="key">共享密钥</label></th>
                        <td>
                            <input id="key" class="input_ss_table" type="password" maxlength="64" autocomplete="new-password" autocorrect="off" autocapitalize="off" spellcheck="false" readonly="readonly">
                            <label class="icmp-key-toggle"><input id="show_key" type="checkbox" disabled="disabled" onchange="toggleKey();"> 显示</label>
                            <div class="icmp-hint">填写服务端生成的 64 位十六进制密钥。</div>
                        </td>
                    </tr>
                    </tbody>
                    </table>
                </div>
                <div id="tablet_2" role="tabpanel" aria-labelledby="show_btn2" style="display:none;">
                    <table class="FormTable icmp-table icmp-config" border="1" cellpadding="4" cellspacing="0" bordercolor="#6b8fa3">
                    <tr><td class="icmp-help">
                        <p>填写落地服务器的 IPv4、TCP 端口和共享密钥，开启开关后点击“提交”。端口及密钥应与服务端一致。</p>
                        <p>开启后，LAN 设备的 IPv4 ICMP 由远端服务器发出；连接或认证失败时直接丢弃，等待连接恢复。</p>
                        <p>查看逐跳路径：Windows 使用 <code>tracert -4 -d 目标IP</code>，Linux 使用 <code>traceroute -I -n 目标IP</code>。</p>
                        <p>转发的 ICMPv6 会被丢弃；路由器本机和同一二层 LAN 内的通信维持正常。关闭开关并提交后恢复原有转发。</p>
                        <p>卸载请使用软件中心的卸载按钮。</p>
                    </td></tr>
                    </table>
                </div>
                <div id="form_message" class="icmp-form-message" role="status" aria-live="polite"></div>
                <div id="apply_button" class="apply_gen"><input id="save" class="button_gen" type="button" value="提交" disabled="disabled" onclick="save();"></div>
            </td></tr>
            </table>
        </td></tr>
        </table>
    </td>
    <td width="10"></td>
</tr>
</table>
<div id="footer"></div>
</body>
</html>
