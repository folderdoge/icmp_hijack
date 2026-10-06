#!/usr/bin/env python3
"""Local browser QA with fixture state and cached ASUS CSS; no router access."""
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
import json

ROOT=Path(__file__).resolve().parents[1]
STATE={'icmp_hijack_enable':'1','icmp_hijack_server':'14.137.20.5','icmp_hijack_port':'39070','icmp_hijack_key':'f'*64,'icmp_hijack_state':'connected','icmp_hijack_status':'已连接服务器，ICMP 隧道工作中','icmp_hijack_status_time':'2026-10-06 20:00:00','icmp_hijack_version':'1.0.7'}
JS="""var tabtitle=[[]],tablink=[[]];function show_menu(fn){fn();document.getElementById('mainMenu').innerHTML='<div style="color:#aab7bd;padding:28px 20px">ASUSWRT<br><br>网络地图<br><br>无线网络<br><br>局域网<br><br>软件中心</div>';}var $={ajax:function(o){fetch(o.url,{method:o.type||'GET',body:o.data}).then(r=>r.json()).then(d=>{if(o.success)o.success(d);if(o.complete)o.complete();}).catch(()=>{if(o.error)o.error();if(o.complete)o.complete();});}};"""

class Handler(BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def send(self,body,kind='text/plain; charset=utf-8'):
        self.send_response(200);self.send_header('Content-Type',kind);self.send_header('Cache-Control','no-store');self.end_headers();self.wfile.write(body)
    def do_GET(self):
        path=self.path.split('?')[0]
        if path in ('/','/Module_icmphijack.asp'):
            self.send((ROOT/'router/webs/Module_icmphijack.asp').read_bytes(),'text/html; charset=utf-8')
        elif path.startswith('/_api/'):
            self.send(json.dumps({'result':[STATE]},ensure_ascii=False).encode(),'application/json; charset=utf-8')
        elif path.endswith('.js'):
            self.send(JS.encode() if path=='/state.js' else b'', 'application/javascript')
        else:
            name=Path(path).name
            if name=='backprevclick.png': name='backprev.png'
            asset=ROOT/'.ui-preview'/name
            self.send(asset.read_bytes() if asset.is_file() else b'', 'text/css' if path.endswith('.css') else 'image/png')
    def do_POST(self):
        count=int(self.headers.get('Content-Length','0'));data=json.loads(self.rfile.read(count) or b'{}')
        STATE.update(data.get('fields',{}))
        STATE['icmp_hijack_state']='connecting' if STATE['icmp_hijack_enable']=='1' else 'disabled'
        STATE['icmp_hijack_status']='正在连接服务器' if STATE['icmp_hijack_enable']=='1' else '劫持已关闭'
        self.send(json.dumps({'result':data.get('id')}).encode(),'application/json')

if __name__=='__main__':
    print('Local UI preview: http://127.0.0.1:8787',flush=True)
    HTTPServer(('127.0.0.1',8787),Handler).serve_forever()
