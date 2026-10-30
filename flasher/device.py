"""One explicit, supported board adapter. No firmware images or vendor keys."""
import base64
import hashlib
import http.client
import ipaddress
import json
from pathlib import Path
import shlex
import time
from urllib.parse import urlencode
import urllib.request
import paramiko


class Device:
    def __init__(self, host, ssh_password, confirm_host, web_password=None):
        ipaddress.ip_address(host)
        self.host = host
        client = self.client = paramiko.SSHClient()
        class Trust(paramiko.MissingHostKeyPolicy):
            def missing_host_key(self, client, hostname, key):
                fingerprint = 'SHA256:' + base64.b64encode(hashlib.sha256(key.asbytes()).digest()).decode().rstrip('=')
                if not confirm_host(hostname, fingerprint):
                    raise RuntimeError('Device identity was not confirmed')
        client.set_missing_host_key_policy(Trust())
        try:
            client.connect(host, username='root', password=ssh_password, timeout=10,
                           auth_timeout=10, look_for_keys=False, allow_agent=False)
        except paramiko.ssh_exception.NoValidConnectionsError:
            if web_password is None:
                raise RuntimeError('SSH is off. Enter the dashboard password to enable it.') from None
            self.enable_ssh(host, web_password)
            for attempt in range(5):
                time.sleep(1)
                try:
                    client.connect(host, username='root', password=ssh_password, timeout=5,
                                   auth_timeout=10, look_for_keys=False, allow_agent=False)
                    break
                except paramiko.ssh_exception.NoValidConnectionsError:
                    if attempt == 4:raise RuntimeError('SSH did not start; no installation files were changed.') from None
        self.token = None

    @staticmethod
    def enable_ssh(host, password):
        address = ipaddress.ip_address(host)
        if address.version != 4 or not address.is_private or address.is_loopback:
            raise ValueError('Automatic SSH setup requires the miner’s private LAN IPv4 address')
        # The stock API is HTTP-only. Refuse redirects and never use system proxies.
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, req, fp, code, msg, headers, newurl):return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        base = 'http://' + host
        def request(path, data=None, token=None):
            headers = {'Content-Type':'application/json'}
            if token:headers['Authorization']='Bearer '+token
            req=urllib.request.Request(base+path, data=data, headers=headers,
                                       method='GET' if data is None else 'PUT')
            with opener.open(req,timeout=8) as response:
                body=response.read(1024*1024)
                return json.loads(body) if body.strip() else None
        info=request('/mcb/status')
        if (info.get('model'),info.get('firmware'),info.get('mcbversion')) != ('Goldshell-KDBox','2.2.1','MCB_V5_4'):
            raise RuntimeError('Unsupported device; SSH settings were not changed')
        try:
            auth=request('/user/login?'+urlencode({'username':'admin','password':password}))
            token=auth.get('JWT Token')
            if not token:raise ValueError('login rejected')
            request('/mcb/protect', b'{}', token)
        except Exception:
            raise RuntimeError('Could not enable SSH. Check the dashboard password and LAN connection.') from None

    def close(self):
        self.client.close()

    def command(self, command):
        _, out, err = self.client.exec_command(command, timeout=20)
        data = out.read().decode('utf-8', 'replace')
        if out.channel.recv_exit_status():
            raise RuntimeError(err.read().decode('utf-8', 'replace') or 'Device command failed')
        return data

    def api(self, path, data=None):
        # HTTP remains inside encrypted SSH and targets only the local native API.
        channel = self.client.get_transport().open_channel('direct-tcpip', ('127.0.0.1', 80), ('127.0.0.1', 0))
        channel.settimeout(10)
        conn = http.client.HTTPConnection('127.0.0.1', timeout=10)
        conn.sock = channel
        headers = {'Content-Type': 'application/json'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        try:
            conn.request('GET' if data is None else 'PUT', path,
                         None if data is None else json.dumps(data), headers)
            res = conn.getresponse()
            body = res.read(1024 * 1024)
            if res.status != 200:
                raise RuntimeError(f'Device API returned HTTP {res.status}')
            return json.loads(body) if body.strip() else None
        finally:
            conn.close()

    def inspect(self):
        info = self.api('/mcb/status')
        if (info.get('model'), info.get('firmware'), info.get('mcbversion')) != ('Goldshell-KDBox', '2.2.1', 'MCB_V5_4'):
            raise RuntimeError('This adapter only supports original KDBox 2.2.1 / MCB_V5_4. No firmware upgrade will be attempted.')
        if self.command('uname -m').strip() != 'armv7l':
            raise RuntimeError('Unsupported CPU architecture')
        return info

    def write(self, remote, data, mode=0o600):
        with self.client.open_sftp() as s:
            with s.open(remote + '.paperclip-new', 'wb') as f:
                f.write(data)
            s.chmod(remote + '.paperclip-new', mode)
        self.command('mv ' + shlex.quote(remote + '.paperclip-new') + ' ' + shlex.quote(remote))

    def backup(self, destination):
        destination = Path(destination)
        destination.mkdir(parents=True, exist_ok=False)
        paths = ['/etc/inittab', '/root/yottamc/index.html', '/root/settings/algosetting.json',
                 '/usr/config/minerd/settings/algosetting.json', '/usr/config/minerd/settings/.algosetting.json',
                 '/usr/config/minerd/settings/cpbsetting.json', '/usr/config/minerd/settings/.cpbsetting.json',
                 '/usr/config/minerd/settings/pools.json', '/usr/config/minerd/settings/.pools.json',
                 '/usr/config/bfgminer/.bfgminer.json', '/usr/config/bfgminer/bfgminer.json',
                 '/etc/init.d/S99paperclip', '/usr/config/paperclip/config.json',
                 '/usr/config/paperclip/approved.json']
        manifest = {}
        with self.client.open_sftp() as s:
            for remote in paths:
                try:
                    info = s.stat(remote)
                except FileNotFoundError:
                    manifest[remote] = {'absent': True}
                    continue
                local = destination / remote.lstrip('/')
                local.parent.mkdir(parents=True, exist_ok=True)
                s.get(remote, str(local))
                manifest[remote] = {'sha256': hashlib.sha256(local.read_bytes()).hexdigest(), 'mode': info.st_mode & 0o777}
        (destination / 'manifest.json').write_text(json.dumps(manifest, indent=2), encoding='utf-8')
        identity={'mac':self.command('cat /sys/class/net/eth0/address').strip(), 'host_key':base64.b64encode(self.client.get_transport().get_remote_server_key().asbytes()).decode()}
        (destination / 'device.json').write_text(json.dumps(identity),encoding='utf-8')
        return destination

    def restore(self, destination):
        destination=Path(destination)
        self.inspect()
        identity=json.loads((destination/'device.json').read_text(encoding='utf-8'))
        if identity['mac']!=self.command('cat /sys/class/net/eth0/address').strip() or identity['host_key']!=base64.b64encode(self.client.get_transport().get_remote_server_key().asbytes()).decode():
            raise RuntimeError('This backup belongs to another miner')
        manifest=json.loads((destination/'manifest.json').read_text(encoding='utf-8'))
        allowed={'/etc/inittab','/root/yottamc/index.html','/root/settings/algosetting.json',
                 '/usr/config/minerd/settings/algosetting.json','/usr/config/minerd/settings/.algosetting.json',
                 '/usr/config/minerd/settings/cpbsetting.json','/usr/config/minerd/settings/.cpbsetting.json',
                 '/usr/config/minerd/settings/pools.json','/usr/config/minerd/settings/.pools.json',
                 '/usr/config/bfgminer/.bfgminer.json','/usr/config/bfgminer/bfgminer.json',
                 '/etc/init.d/S99paperclip','/usr/config/paperclip/config.json','/usr/config/paperclip/approved.json'}
        for remote,meta in manifest.items():
            if remote not in allowed:raise RuntimeError('Unexpected recovery path')
            if not meta.get('absent') and hashlib.sha256((destination/remote.lstrip('/')).read_bytes()).hexdigest()!=meta['sha256']:
                raise RuntimeError('Recovery backup checksum mismatch')
        # Restore boot configuration last. Do not restart until all files are written.
        for remote,meta in sorted(manifest.items(),key=lambda item:item[0]=='/etc/inittab'):
            if not meta.get('absent'):self.write(remote,(destination/remote.lstrip('/')).read_bytes(),meta['mode'])
        if manifest.get('/etc/init.d/S99paperclip',{}).get('absent'):
            self.command('mv /etc/init.d/S99paperclip /usr/config/paperclip/disabled-startup-hook')
        self.command('sync; reboot')

    def install(self, root, password, backup_dir, progress=print):
        root = Path(root)
        self.inspect()
        binary = (root / 'build/paperclip-controller').read_bytes()
        if binary[:4] != b'\x7fELF':
            raise RuntimeError('Missing ARM controller build')
        self.token = self.api('/user/login?' + urlencode({'username': 'admin', 'password': password})).get('JWT Token')
        if not self.token:
            raise RuntimeError('Miner dashboard password was not accepted')
        if self.command('test ! -f /usr/config/paperclip/pending.json && echo clear').strip() != 'clear':
            raise RuntimeError('Finish the active tuning trial first')
        progress('Saving a private recovery backup…')
        backup = self.backup(backup_dir)

        init = (backup / 'etc/inittab').read_text(encoding='utf-8')
        init = init.replace('ttyAMA3::respawn:xminerd', 'ttyAMA3::respawn:/usr/config/paperclip/launcher.sh')
        if 'respawn:/usr/config/paperclip/launcher.sh' not in init:
            raise RuntimeError('Unexpected startup layout; no changes made')
        progress('Installing controller and original-port dashboard…')
        self.command('mkdir -p /usr/config/paperclip; chmod 700 /usr/config/paperclip')
        self.write('/usr/config/paperclip/controller', binary, 0o700)
        expected = hashlib.sha256(binary).hexdigest()
        if self.command('sha256sum /usr/config/paperclip/controller').split()[0] != expected:
            raise RuntimeError('Controller transfer verification failed')
        for name in ['launcher.sh', 'supervisor.sh', 'boot.sh']:
            self.write('/usr/config/paperclip/' + name, (root / 'controller' / name).read_bytes(), 0o700)
        self.write('/usr/config/paperclip/config.json', json.dumps({'admin_password': password}).encode())
        approved = {'clock': 740, 'millivolts': 430, 'fan': 100, 'mode': 'fixed'}
        if not (backup / 'usr/config/paperclip/approved.json').exists():
            self.write('/usr/config/paperclip/approved.json', json.dumps(approved).encode())
        if self.command('test -f /root/yottamc/legacy.html && echo exists; true').strip() != 'exists':
            self.write('/root/yottamc/legacy.html', (backup / 'root/yottamc/index.html').read_bytes(), 0o644)
        for asset in (root / 'controller/web').iterdir():
            self.write('/root/yottamc/' + asset.name, asset.read_bytes(), 0o644)
        menu = self.api('/mcb/algosetting')
        menu['algos'] = [{'name': 'blake2s(KDA)', 'id': 0}, {'name': 'blake2b(SC)', 'id': 1}]
        for name in ['/root/settings/algosetting.json', '/usr/config/minerd/settings/algosetting.json', '/usr/config/minerd/settings/.algosetting.json']:
            self.write(name, json.dumps(menu).encode(), 0o644)
        self.api('/mcb/algosetting', menu)
        self.write('/etc/inittab', init.encode(), 0o644)
        self.write('/etc/init.d/S99paperclip', b'#!/bin/sh\ncase "$1" in start) /usr/config/paperclip/boot.sh </dev/null >/tmp/paperclip-start.log 2>&1 & ;; esac\n', 0o755)
        self.command('if pidof controller >/dev/null; then kill -TERM $(pidof controller); else nohup /usr/config/paperclip/boot.sh </dev/null >/tmp/paperclip-start.log 2>&1 & fi; echo started')
        progress('Installed. Configure mining pools at http://' + self.host + '/ . Recovery backup: ' + str(backup))
        return backup
