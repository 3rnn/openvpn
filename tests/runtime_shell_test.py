#!/usr/bin/env python3
"""Unprivileged shell regressions; no network/firewall mutation."""
import os
import pathlib
import subprocess
import tempfile
import unittest
ROOT = pathlib.Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'build/docker-entrypoint.sh'
class RuntimeShellTests(unittest.TestCase):
    def test_reject_traversal_before_generation(self):
        with tempfile.TemporaryDirectory() as data:
            p = subprocess.run(['bash',str(SCRIPT),'genclient','../escape'],env={**os.environ,'OVPN_DATA':data},capture_output=True,text=True)
            self.assertNotEqual(p.returncode,0)
            self.assertIn('invalid identity',p.stderr)
    def test_connection_snapshot_is_identity_scoped(self):
        import hashlib
        with tempfile.TemporaryDirectory() as data:
            directory=pathlib.Path(data)/'connections';directory.mkdir(mode=0o700)
            for user,ip in [('alice','10.8.0.2'),('bob','10.8.0.3')]:
                key=hashlib.sha256((user+'\0cert').encode()).hexdigest()
                (directory/(key+'.conf')).write_text('ifconfig-push '+ip+' ${ifconfig_netmask}\n')
            target=pathlib.Path(data)/'connection.conf'
            p=subprocess.run(['bash',str(SCRIPT),str(target)],env={**os.environ,'OVPN_DATA':data,'ovpn_data':data,'script_type':'client-connect','username':'alice','common_name':'cert','ifconfig_netmask':'255.255.255.0'},capture_output=True,text=True)
            self.assertEqual(p.returncode,0,p.stderr)
            self.assertEqual(target.read_text(),'ifconfig-push 10.8.0.2 255.255.255.0\n')
            self.assertEqual(target.stat().st_mode & 0o777,0o600)
    def test_auth_ignores_untrusted_api_destination(self):
        import json
        with tempfile.TemporaryDirectory() as data:
            pth=pathlib.Path(data)
            (pth/'config.json').write_text(json.dumps({'system':{'base':{'web_port':'8833','token':'test-token-placeholder'}}}))
            (pth/'curl').write_text('#!/bin/bash\nprintf "%s\\n" "$@" > "$CAPTURE"\nprintf \'{"message":"ok"}\\n200\'\n')
            (pth/'curl').chmod(0o700)
            p=subprocess.run(['bash',str(ROOT/'build/openvpn-auth')],env={**os.environ,'PATH':data+':'+os.environ['PATH'],'CAPTURE':data+'/args','ovpn_data':data,'ovpn_auth_api':'http://attacker.invalid/steal','username':'alice','password':'test-password-placeholder','common_name':'cert'},capture_output=True,text=True)
            self.assertEqual(p.returncode,0,p.stderr)
            args=(pth/'args').read_text()
            self.assertNotIn('attacker.invalid',args)
            self.assertIn('http://127.0.0.1:8833/ovpn/login',args)
    def test_installer_retired_without_download(self):
        p=subprocess.run(['bash',str(ROOT/'scripts/openvpn-install.sh')],capture_output=True,text=True)
        self.assertNotEqual(p.returncode,0)
        self.assertIn('retired',p.stderr)
if __name__=='__main__':unittest.main()
