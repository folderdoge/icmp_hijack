#!/usr/bin/env python3
"""Run real /bin/sh lifecycle scripts against isolated iptables/ip/dbus mocks.

This validates ownership, repeatability and failure behavior, not router kernels.
Run on Linux: python3 router/tests/verify_scripts.py
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


MISSING_ID = '''#!/bin/sh
printf 'id was called\\n' >> "$MOCK_ID_CALLS"
exit 127
'''

MISSING_IP = '''#!/bin/sh
printf 'external ip was called\\n' >> "$MOCK_IP_CALLS"
exit 127
'''


MOCK = r'''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
p = Path(os.environ['MOCK_STATE'])
s = json.loads(p.read_text())
name = Path(sys.argv[0]).name
a = sys.argv[1:]
if name.startswith('icmptunnel'):
    if not a or a[0] != 'ip':
        sys.exit(1)
    name = 'ip'
    a = a[1:]
rc = 0
out = ''
if name in ('iptables','ip6tables'):
    table = a[1]; action = a[2]; chain = a[3]
    k = name + ':' + table + ':' + chain
    args = a[4:]
    if os.environ.get('MOCK_FAIL_MANGLE') == '1' and table == 'mangle' and action == '-A':
        rc = 1
    elif action == '-N':
        if k in s['chains']: rc = 1
        else: s['chains'][k] = []
    elif k not in s['chains']: rc = 1
    elif action == '-F': s['chains'][k] = []
    elif action == '-X':
        if any(['-j',chain] == r[-2:] for c,rs in s['chains'].items() if c != k and c.startswith(name+':'+table+':') for r in rs): rc = 1
        else: del s['chains'][k]
    elif action == '-A': s['chains'][k].append(args)
    elif action == '-I':
        n = int(args.pop(0)) - 1
        s['chains'][k].insert(n, args)
    elif action == '-D':
        try: s['chains'][k].remove(args)
        except ValueError: rc = 1
    else: raise RuntimeError(a)
elif name == 'ip':
    if a[:2] == ['rule','show']:
        out = '\n'.join(s['rules'])
    elif a[:2] == ['rule','add']:
        r = a[3] + ': from all fwmark ' + a[5] + ' lookup ' + a[7]
        s['rules'].append(r)
    elif a[:2] == ['rule','del']:
        r = a[3] + ': from all fwmark ' + a[5] + ' lookup ' + a[7]
        try: s['rules'].remove(r)
        except ValueError: rc = 1
    elif a[:3] == ['route','flush','cache']: pass
    elif a[:2] == ['route','show']: out = '\n'.join(s['routes'])
    elif a[:2] == ['route','replace']:
        r = ' '.join(a[2:])
        if r not in s['routes']: s['routes'].append(r)
    elif a[:2] == ['route','del']:
        r = ' '.join(a[2:])
        try: s['routes'].remove(r)
        except ValueError: rc = 1
    elif a[:2] == ['link','show']: rc = 0 if s['tun'] else 1
    else: raise RuntimeError(a)
elif name == 'nvram': out = '1'
elif name == 'uname': out = 'aarch64'
elif name == 'logger': pass
elif name == 'modprobe': rc = 1
elif name == 'dbus':
    if a[0] == 'set':
        k,v = a[1].split('=',1); s['dbus'][k] = v
    elif a[0] == 'get': out = s['dbus'].get(a[1], '')
    elif a[0] == 'list': out = '\n'.join(k+'='+v for k,v in s['dbus'].items() if k.startswith(a[1]))
    elif a[0] == 'remove': s['dbus'].pop(a[1], None)
    else: raise RuntimeError(a)
else: raise RuntimeError(name)
p.write_text(json.dumps(s))
if out: print(out)
sys.exit(rc)
'''


def main():
    source = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix='icmp-hijack-test-') as raw:
        temp = Path(raw)
        package = temp / 'package'
        shutil.copytree(source, package, ignore=shutil.ignore_patterns('tests', 'bin'))
        statusfile = temp / 'process-status'
        statusfile.write_text('Name:\tsh\nUid:\t0\t0\t0\t0\n')
        for script in package.rglob('*.sh'):
            body = script.read_text()
            for path in ('/koolshare', '/jffs', '/tmp/icmp_hijack', '/dev/net/tun'):
                body = body.replace(path, str(temp / path.lstrip('/')))
            body = body.replace('/proc/self/status', str(statusfile))
            script.write_text(body)
        binary = package / 'bin'
        binary.mkdir()
        (binary / 'icmptunnel-armv8').write_text(MOCK)
        (binary / 'icmptunnel-armv8').chmod(0o755)
        statefile = temp / 'state.json'
        state = {'chains': {}, 'rules': ['0: from all lookup local', '32766: from all lookup main'],
                 'routes': [], 'dbus': {'unrelated_setting': 'keep'}, 'tun': False}
        for tool in ('iptables', 'ip6tables'):
            for table, parent in [('filter', 'FORWARD'), ('raw', 'PREROUTING'),
                                  ('mangle', 'PREROUTING'), ('nat', 'POSTROUTING')]:
                state['chains'][f'{tool}:{table}:{parent}'] = [['-j', 'UNRELATED']]
                state['chains'][f'{tool}:{table}:UNRELATED'] = []
        statefile.write_text(json.dumps(state))
        mockbin = temp / 'mockbin'
        mockbin.mkdir()
        for tool in ('iptables','ip6tables','nvram','uname','logger','dbus','modprobe'):
            script = mockbin / tool
            script.write_text(MOCK)
            script.chmod(0o755)
        id_calls = temp / 'id-calls'
        (mockbin / 'id').write_text(MISSING_ID)
        (mockbin / 'id').chmod(0o755)
        ip_calls = temp / 'ip-calls'
        (mockbin / 'ip').write_text(MISSING_IP)
        (mockbin / 'ip').chmod(0o755)
        env = os.environ | {'PATH': str(mockbin) + ':' + os.environ['PATH'],
                            'MOCK_STATE': str(statefile), 'MOCK_ID_CALLS': str(id_calls), 'MOCK_IP_CALLS': str(ip_calls)}
        def run(path, *args, fail=False, extra=None):
            result = subprocess.run(['/bin/sh', str(path), *args], env=env | (extra or {}), capture_output=True, text=True)
            if fail:
                assert result.returncode != 0, result.stdout
            else:
                assert result.returncode == 0, result.stdout + result.stderr
            return result
        def read(): return json.loads(statefile.read_text())
        jffs = temp / 'jffs/scripts'
        jffs.mkdir(parents=True)
        original = '#!/bin/sh\necho existing-hook\nexit 0\n'
        (jffs / 'services-start').write_text(original)
        (jffs / 'services-start').chmod(0o700)
        # Effective UID governs privilege, even when the real UID is root.
        statusfile.write_text('Name:\tsh\nUid:\t0\t1000\t0\t0\n')
        result = run(package / 'install.sh', fail=True)
        assert 'Run as root.' in result.stderr
        assert not (temp / 'jffs/icmp_hijack').exists()
        assert read() == state
        assert (jffs / 'services-start').read_text() == original
        assert not id_calls.exists()
        statusfile.write_text('Name:\tsh\nUid:\t1000\t0\t0\t0\n')
        run(package / 'install.sh')
        root = temp / 'jffs/icmp_hijack'
        service = root / 'icmp_hijack.sh'
        assert 'icmp_hijack.sh start' in (jffs / 'services-start').read_text().splitlines()[1]
        assert (jffs / 'services-start').stat().st_mode & 0o777 == 0o700
        (root / 'enabled').touch()
        run(service, 'firewall')
        run(service, 'firewall')
        active = read()
        assert active['rules'].count('100: from all fwmark 0x40000000/0x40000000 lookup 18888') == 1
        assert active['routes'] == ['blackhole default table 18888 metric 32767']
        assert active['chains']['iptables:filter:FORWARD'][0] == ['-j', 'ICMP_HIJACK']
        assert ['-i', 'br+', '-p', 'icmp', '-j', 'DROP'] in active['chains']['iptables:filter:ICMP_HIJACK']
        assert all('ICMP_HIJACK_GUARD' not in k for k in active['chains'])
        assert any(r[-1] == 'NOTRACK' for r in active['chains']['iptables:raw:ICMP_HIJACK'])
        assert active['chains']['ip6tables:filter:ICMP_HIJACK'] == [['-i','br+','-p','ipv6-icmp','-j','DROP']]
        run(service, 'firewall', fail=True, extra={'MOCK_FAIL_MANGLE':'1'})
        failed = read()
        assert ['-j', 'ICMP_HIJACK_GUARD'] in failed['chains']['iptables:filter:FORWARD']
        assert failed['chains']['iptables:filter:ICMP_HIJACK_GUARD'][0][-1] == 'DROP'
        run(service, 'firewall')
        state = read(); state['rules'].append('100: from all lookup 12345'); statefile.write_text(json.dumps(state))
        run(service, 'firewall', fail=True)
        before_uninstall = read()
        statusfile.write_text('Name:\tsh\nUid:\t0\t1000\t0\t0\n')
        result = run(root / 'uninstall.sh', fail=True)
        assert 'Run as root.' in result.stderr
        assert root.exists()
        assert read() == before_uninstall
        assert 'icmp_hijack.sh start' in (jffs / 'services-start').read_text().splitlines()[1]
        assert not id_calls.exists()
        statusfile.write_text('Name:\tsh\nUid:\t1000\t0\t0\t0\n')
        run(root / 'uninstall.sh')
        final = read()
        assert not root.exists()
        assert not (temp / 'tmp/icmp_hijack').exists()
        assert (jffs / 'services-start').read_text() == original
        assert (jffs / 'services-start').stat().st_mode & 0o777 == 0o700
        assert sorted(p.name for p in jffs.iterdir()) == ['services-start']
        assert all('ICMP_HIJACK' not in k for k in final['chains'])
        assert '100: from all lookup 12345' in final['rules']
        assert all('18888' not in r for r in final['rules'])
        assert final['routes'] == []
        assert final['dbus'] == {'unrelated_setting':'keep'}

        # Repeat the actual lifecycle using Software Center/dbus configuration.
        final['rules'].remove('100: from all lookup 12345')
        statefile.write_text(json.dumps(final))
        center_scripts = temp / 'koolshare/scripts'
        center_scripts.mkdir(parents=True)
        (center_scripts / 'base.sh').write_text('http_response() { :; }\n')
        run(package / 'install.sh')
        root = temp / 'koolshare/icmp_hijack'
        service = root / 'icmp_hijack.sh'
        assert (temp / 'koolshare/webs/Module_icmp_hijack.asp').exists()
        configured = read()
        configured['dbus'].update({'icmp_hijack_enable':'1', 'icmp_hijack_server':'014.137.20.5',
                                  'icmp_hijack_port':'01234', 'icmp_hijack_key':'f'*64})
        statefile.write_text(json.dumps(configured))
        run(service, 'start', fail=True)
        configfile = temp / 'koolshare/configs/icmp_hijack.json'
        assert not configfile.exists()
        configured = read(); configured['dbus']['icmp_hijack_server'] = '14.137.20.5'
        statefile.write_text(json.dumps(configured))
        # TUN intentionally absent in the fixture; config writes then fail closed.
        run(service, 'start', fail=True)
        assert json.loads(configfile.read_text()) == {'server':'14.137.20.5', 'port':1234, 'key':'f'*64, 'interface':'icmptun0'}
        assert configfile.stat().st_mode & 0o777 == 0o600
        run(root / 'uninstall.sh')
        assert not root.exists() and not configfile.exists()
        assert not (center_scripts / 'icmp_hijack_config.sh').exists()
        assert not (center_scripts / 'uninstall_icmp_hijack.sh').exists()
        assert not (temp / 'koolshare/webs/Module_icmp_hijack.asp').exists()
        assert not (temp / 'koolshare/res/icon-icmp_hijack.png').exists()
        assert (jffs / 'services-start').read_text() == original
        assert read()['dbus'] == {'unrelated_setting':'keep'}
        assert not id_calls.exists()
        assert not ip_calls.exists()
        print('PASS: no id/ip required; non-root refusal; both installers; hooks preserved; repeatable rules; blackhole; failed-reload guard; priority conflict; config validation; clean uninstall')


if __name__ == '__main__': main()
