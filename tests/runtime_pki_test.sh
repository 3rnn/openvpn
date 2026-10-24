#!/bin/bash
# Run ONLY in a disposable network-none container. Creates test PKI under /data.
# No production data, privilege, TUN, or firewall mutation is required.
set -euo pipefail
umask 077
[[ ! -e /data/config.json ]] || { printf 'refusing nonempty test data\n' >&2; exit 1; }
install -d -m 0700 /data
cat > /data/config.json <<'JSON'
{"system":{"base":{"server_name":"server_test","server_cn":"ca_test","web_port":"8833","nft_table_name":"openvpn_test"}},"openvpn":{"ovpn_port":1194,"ovpn_proto":"udp","ovpn_subnet":"10.8.0.0/24","ovpn_management":"127.0.0.1:7505"}}
JSON
# PKI/config initialization occurs before nft load; without NET_ADMIN startup
# must stop at nft, while leaving a real generated PKI for permission checks.
if /usr/bin/docker-entrypoint.sh /usr/sbin/openvpn > /data/init-test.log 2>&1;then
 printf 'unexpected privileged startup\n' >&2;exit 1
fi
[[ -s /data/pki/private/ca.key && -s /data/pki/private/server_test.key && -s /data/pki/tc.key && -s /data/server.conf ]]
/usr/bin/docker-entrypoint.sh genclient alice vpn.example.invalid 1194 '' '' false > /data/client-test.log 2>&1
[[ -s /data/pki/private/alice.key && -s /data/clients/alice.ovpn ]]
if grep -q '^static-challenge ' /data/clients/alice.ovpn;then
 printf 'unexpected MFA challenge for false flag\n' >&2;exit 1
fi
# Exercise the actual shell positional boundary: profile config, CCD, then MFA.
/usr/bin/docker-entrypoint.sh genclient bob vpn.example.invalid 1194 'route 10.20.0.0 255.255.0.0' 'iroute 10.30.0.0 255.255.0.0' true > /data/mfa-client-test.log 2>&1
[[ $(cat /data/ccd/bob) == 'iroute 10.30.0.0 255.255.0.0' ]]
grep -qx 'route 10.20.0.0 255.255.0.0' /data/clients/bob.ovpn
grep -qx 'static-challenge "Enter MFA code" 1' /data/clients/bob.ovpn
[[ $(stat -c '%a' /data/clients/bob.ovpn) == 600 ]]
[[ $(stat -c '%a' /data/ccd/bob) == 600 ]]
[[ -z $(find /data/pki -type d ! -perm 0700 -print) ]]
[[ -z $(find /data/pki -type f ! -perm 0600 -print) ]]
[[ $(stat -c '%a' /data/clients/alice.ovpn) == 600 ]]
[[ $(stat -c '%a' /data/clients) == 700 ]]
# Failed unsafe name must not create any outside profile.
if /usr/bin/docker-entrypoint.sh genclient ../escape vpn.example.invalid 1194 '' '' false 2>/dev/null;then exit 1;fi
printf 'PASS: actual EasyRSA CA/server/client/tls-crypt generation; PKI dirs0700 files0600 and profile0600; unsafe name rejected\n'
printf 'Runtime package inventory:\n'
dpkg-query -W -f='${Package} ${Version}\n' openvpn easy-rsa openssl nftables supervisor ca-certificates
printf 'Runtime excludes compiler and git: '
! command -v go && ! command -v gcc && ! command -v git
printf 'PASS\n'
