#!/bin/bash
# All state is private. Hooks operate only on policy bound to authenticated
# username AND certificate name; OpenVPN supplies a per-connection target file.
set -euo pipefail
umask 077
OVPN_DATA=${OVPN_DATA:-${ovpn_data:-/data}}
[[ "$OVPN_DATA" =~ ^/[A-Za-z0-9_./-]+$ ]] || { printf 'invalid data path\n' >&2; exit 1; }
SYSTEM_CONFIG="$OVPN_DATA/config.json"
export EASYRSA_PKI="$OVPN_DATA/pki"
export EASYRSA_BATCH=1
fail() { printf '%s\n' "$*" >&2; exit 1; }
identity() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9_.@-]{0,127}$ ]] || fail 'invalid identity'; }
port() {
 if ! [[ "$1" =~ ^[0-9]{1,5}$ ]]; then fail 'invalid port'; fi
 if ! ((10#$1 > 0 && 10#$1 <= 65535)); then fail 'invalid port'; fi
}
ipv4() {
 local value=$1 a b c d oct
 [[ "$value" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || fail 'invalid IPv4'
 IFS=. read -r a b c d <<< "$value"
 for oct in "$a" "$b" "$c" "$d"; do ((10#$oct <= 255)) || fail 'invalid IPv4'; done
}
private_state() {
 install -d -m 0700 "$OVPN_DATA" "$OVPN_DATA/clients" "$OVPN_DATA/ccd" "$OVPN_DATA/connections"
 if [[ -d "$EASYRSA_PKI" ]]; then
  find "$EASYRSA_PKI" -type d -exec chmod 0700 {} +
  find "$EASYRSA_PKI" -type f -exec chmod 0600 {} +
 fi
 find "$OVPN_DATA/clients" "$OVPN_DATA/ccd" "$OVPN_DATA/connections" -type f -exec chmod 0600 {} +
}
validate_data_config() {
 local line option
 [[ ${#1} -le 65536 ]] || fail 'configuration too large'
 while IFS= read -r line || [[ -n "$line" ]]; do
  [[ "$line" != *$'\r'* && "$line" != *$'\\'* ]] || fail 'invalid client configuration'
  read -r option _ <<< "$line"
  case "$option" in
   ''|'#'*|';'*) ;;
   push)
    [[ "$line" =~ ^[[:space:]]*push[[:space:]]+\"(route|route-ipv6|dhcp-option|redirect-gateway|redirect-private|block-outside-dns|topology|ping|ping-restart|inactive|tun-mtu|mssfix)([[:space:]][^\"]*)?\"[[:space:]]*$ ]] || fail 'invalid pushed option' ;;
   route|route-ipv6|iroute|iroute-ipv6|ifconfig-push|ifconfig-ipv6-push|push-reset|push-remove|disable) ;;
   *) fail 'unsupported client directive' ;;
  esac
 done <<< "$1"
}
getsubnet() {
 local addr=${1%/*} bits=${1#*/} mask='' byte
 ipv4 "$addr"
 if ! [[ "$bits" =~ ^[0-9]{1,2}$ ]]; then fail 'invalid IPv4 prefix'; fi
 if ! ((10#$bits <= 32)); then fail 'invalid IPv4 prefix'; fi
 bits=$((10#$bits))
 for _ in 1 2 3 4; do
  if ((bits>=8)); then byte=255; bits=$((bits-8)); else byte=$((256-2**(8-bits)));bits=0;fi
  mask+="${mask:+.}$byte"
 done
 printf '%s %s\n' "$addr" "$mask"
}
load_settings() {
 SERVER_NAME=$(jq -er '.system.base.server_name' "$SYSTEM_CONFIG"); identity "$SERVER_NAME"
 SERVER_CN=$(jq -er '.system.base.server_cn' "$SYSTEM_CONFIG"); identity "$SERVER_CN"
 OVPN_PORT=$(jq -r '.openvpn.ovpn_port // 1194' "$SYSTEM_CONFIG"); port "$OVPN_PORT"
 WEB_PORT=$(jq -r '.system.base.web_port // "8833"' "$SYSTEM_CONFIG");port "$WEB_PORT"
 OVPN_PROTO=$(jq -r '.openvpn.ovpn_proto // "udp"' "$SYSTEM_CONFIG")
 case "$OVPN_PROTO" in udp|udp4|udp6|tcp-server|tcp4-server|tcp6-server) ;;*)fail 'invalid protocol';;esac
 OVPN_MAXCLIENTS=$(jq -r '.openvpn.ovpn_max_clients // 200' "$SYSTEM_CONFIG")
 if ! [[ "$OVPN_MAXCLIENTS" =~ ^[0-9]{1,6}$ ]]; then fail 'invalid client count'; fi
 if ! ((10#$OVPN_MAXCLIENTS>0)); then fail 'invalid client count'; fi
 OVPN_MANAGEMENT=$(jq -r '.openvpn.ovpn_management // "127.0.0.1:7505"' "$SYSTEM_CONFIG")
 [[ "$OVPN_MANAGEMENT" =~ ^127\.0\.0\.1:[0-9]{1,5}$ ]] || fail 'management must use loopback';port "${OVPN_MANAGEMENT##*:}"
 OVPN_SUBNET=$(jq -r '.openvpn.ovpn_subnet // "10.8.0.0/24"' "$SYSTEM_CONFIG")
 SUBNET=$(getsubnet "$OVPN_SUBNET")
 OVPN_IPV6=$(jq -r '.openvpn.ovpn_ipv6 // false' "$SYSTEM_CONFIG")
 OVPN_SUBNET6=$(jq -r '.openvpn.ovpn_subnet6 // "fdaf:f178:e916:6dd0::/64"' "$SYSTEM_CONFIG")
 [[ "$OVPN_IPV6" == false || "$OVPN_IPV6" == true ]] || fail 'invalid IPv6 flag'
 [[ "$OVPN_SUBNET6" =~ ^[a-fA-F0-9:]+/[0-9]{1,3}$ ]] || fail 'invalid IPv6 subnet'
 OVPN_GATEWAY=$(jq -r '.openvpn.ovpn_gateway // false' "$SYSTEM_CONFIG")
 [[ "$OVPN_GATEWAY" == false || "$OVPN_GATEWAY" == true ]] || fail 'invalid gateway flag'
}
init_pki() {
 load_settings
 [[ ! -e "$EASYRSA_PKI" ]] || fail 'existing partial PKI: recover backup; refusing replacement'
 /usr/share/easy-rsa/easyrsa init-pki
 cat > "$EASYRSA_PKI/vars" <<'EOF'
set_var EASYRSA /usr/share/easy-rsa
set_var EASYRSA_CA_EXPIRE 365
set_var EASYRSA_CERT_EXPIRE 365
set_var EASYRSA_CRL_DAYS 365
set_var EASYRSA_ALGO ec
set_var EASYRSA_CURVE prime256v1
EOF
 /usr/share/easy-rsa/easyrsa --batch --req-cn="$SERVER_CN" build-ca nopass
 /usr/share/easy-rsa/easyrsa --batch build-server-full "$SERVER_NAME" nopass
 /usr/share/easy-rsa/easyrsa --batch gen-crl
 /usr/sbin/openvpn --genkey secret "$EASYRSA_PKI/tc.key"
 private_state
}
init_config() {
 load_settings
 local temp
 temp=$(mktemp "$OVPN_DATA/.server.XXXXXXXX")
 trap 'rm -f -- "${temp:-}"' EXIT
 cat > "$temp" <<EOF
port $OVPN_PORT
proto $OVPN_PROTO
dev tun
persist-key
persist-tun
keepalive 10 60
topology subnet
server $SUBNET
dh none
tls-groups prime256v1
tls-crypt $EASYRSA_PKI/tc.key
crl-verify $EASYRSA_PKI/crl.pem
ca $EASYRSA_PKI/ca.crt
cert $EASYRSA_PKI/issued/$SERVER_NAME.crt
key $EASYRSA_PKI/private/$SERVER_NAME.key
auth SHA256
data-ciphers AES-128-GCM
tls-server
tls-version-min 1.2
auth-user-pass-verify /usr/lib/openvpn/plugins/openvpn-auth via-env
client-disconnect /usr/bin/docker-entrypoint.sh
client-connect /usr/bin/docker-entrypoint.sh
learn-address /usr/bin/docker-entrypoint.sh
script-security 3
status $OVPN_DATA/openvpn-status.log
client-config-dir $OVPN_DATA/ccd
duplicate-cn
client-to-client
max-clients $OVPN_MAXCLIENTS
management ${OVPN_MANAGEMENT/:/ }
verb 2
setenv ovpn_data $OVPN_DATA
setenv auth_api http://127.0.0.1:$WEB_PORT/login
setenv ovpn_auth_api http://127.0.0.1:$WEB_PORT/ovpn/login
setenv ovpn_history_api http://127.0.0.1:$WEB_PORT/ovpn/history
EOF
 [[ "$OVPN_IPV6" == false ]] || printf 'server-ipv6 %s\n' "$OVPN_SUBNET6" >> "$temp"
 [[ "$OVPN_GATEWAY" == false ]] || printf '%s\n' 'push "dhcp-option DNS 8.8.8.8"' 'push "redirect-gateway def1 bypass-dhcp"' >> "$temp"
 [[ "$OVPN_PROTO" != udp* ]] || printf 'explicit-exit-notify 1\n' >> "$temp"
 mv -f -- "$temp" "$OVPN_DATA/server.conf"
 trap - EXIT
}
genclient() {
 identity "$1"
 load_settings
 local name=$1 host=${2:-} clientport=${3:-$OVPN_PORT} config=${4:-} mfa=${5:-false} temp proto
 [[ "$host" =~ ^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$ ]] || fail 'explicit valid remote hostname/IP required'
 port "$clientport";validate_data_config "$config"
 [[ "$mfa" == false || "$mfa" == true ]] || fail 'invalid MFA flag'
 # EasyRSA mutates a shared index; serialize issuance with a private flock.
 exec 9>"$OVPN_DATA/.pki.lock";flock -x 9
 if [[ ! -f "$EASYRSA_PKI/private/$name.key" ]];then /usr/share/easy-rsa/easyrsa --batch build-client-full "$name" nopass >/dev/null;fi
 private_state
 temp=$(mktemp "$OVPN_DATA/clients/.profile.XXXXXXXX")
 trap 'rm -f -- "${temp:-}"' EXIT
 proto=${OVPN_PROTO/-server/-client}
 cat > "$temp" <<EOF
client
proto $proto
remote $host $clientport
dev tun
resolv-retry infinite
nobind
persist-key
persist-tun
remote-cert-tls server
verify-x509-name $SERVER_NAME name
auth SHA256
auth-user-pass
data-ciphers AES-128-GCM
tls-client
tls-version-min 1.2
verb 3
EOF
 [[ "$mfa" == false ]] || printf '%s\n' 'static-challenge "Enter MFA code" 1' >> "$temp"
 printf '\n%s\n' "$config" >> "$temp"
 { printf '<ca>\n';cat "$EASYRSA_PKI/ca.crt";printf '</ca>\n<cert>\n';openssl x509 -in "$EASYRSA_PKI/issued/$name.crt";printf '</cert>\n<key>\n';cat "$EASYRSA_PKI/private/$name.key";printf '</key>\n<tls-crypt>\n';cat "$EASYRSA_PKI/tc.key";printf '</tls-crypt>\n'; } >> "$temp"
 mv -f -- "$temp" "$OVPN_DATA/clients/$name.ovpn"
 trap - EXIT
}
client_connect() {
 local key source policy target=$1 temp mask=${ifconfig_netmask:-}
 identity "${username:-}";identity "${common_name:-}";ipv4 "$mask"
 key=$(printf '%s\0%s' "$username" "$common_name" | sha256sum);key=${key%% *}
 source="$OVPN_DATA/connections/$key.conf"
 [[ -f "$source" && ! -L "$source" ]] || fail 'authenticated connection policy missing'
 policy=$(cat -- "$source")
 # Replace the literal policy placeholder, not a shell expansion.
 # shellcheck disable=SC2016
 policy=${policy//'${ifconfig_netmask}'/$mask}
 validate_data_config "$policy"
 [[ -f "$target" && -L "$target" ]] && fail 'invalid connection target'
 temp=$(mktemp "${target}.XXXXXXXX")
 trap 'rm -f -- "${temp:-}"' EXIT
 printf '%s\n' "$policy" > "$temp"
 chmod 0600 "$temp"
 mv -f -- "$temp" "$target"
 trap - EXIT
}
post_hook() {
 local endpoint=$1 token web
 shift
 web=$(jq -er '.system.base.web_port' "$SYSTEM_CONFIG");port "$web"
 token=$(jq -er '.system.base.token' "$SYSTEM_CONFIG")
 [[ "$token" != *$'\r'* && "$token" != *$'\n'* ]] || fail 'invalid hook token'
 curl --fail --silent --show-error --connect-timeout 5 --max-time 10 --noproxy '*' \
  -H "O-Token: $token" -o /dev/null "http://127.0.0.1:$web/$endpoint" "$@"
}
set_firewall() { post_hook 'ovpn/firewall?a=add_ovips' --data-urlencode "vip=${ifconfig_pool_remote_ip:-}" --data-urlencode "vip6=${ifconfig_pool_remote_ip6:-}" --data-urlencode "username=${username:-}"; }
client_disconnect() {
 post_hook 'ovpn/firewall?a=delete_ovips' --data-urlencode "vip=${ifconfig_pool_remote_ip:-}" --data-urlencode "vip6=${ifconfig_pool_remote_ip6:-}" --data-urlencode "username=${username:-}" || printf 'firewall cleanup failed\n' >&2
 local args=() item
 for item in vip vip6 rip rip6 common_name username bytes_received bytes_sent time_unix time_duration;do
  case "$item" in vip)args+=(--data-urlencode "vip=${ifconfig_pool_remote_ip:-}");;vip6)args+=(--data-urlencode "vip6=${ifconfig_pool_remote_ip6:-}");;rip)args+=(--data-urlencode "rip=${trusted_ip:-}");;rip6)args+=(--data-urlencode "rip6=${trusted_ip6:-}");;*)args+=(--data-urlencode "$item=${!item-}");;esac
 done
 post_hook ovpn/history "${args[@]}" || printf 'history submission failed\n' >&2
}
load_nftconfig() {
 local table
 table=$(jq -r '.system.base.nft_table_name // "openvpn-nft"' "$SYSTEM_CONFIG")
 [[ "$table" =~ ^[A-Za-z_][A-Za-z0-9_-]{0,63}$ ]] || fail 'invalid nft table'
 if [[ ! -f "$OVPN_DATA/openvpn.nft" ]];then
 cat > "$OVPN_DATA/openvpn.nft" <<EOF
table inet $table {
 set blacklist_v4 { type ipv4_addr; }
 set blacklist_v6 { type ipv6_addr; }
 chain forward { type filter hook forward priority filter; policy accept; ip saddr @blacklist_v4 drop; ip6 saddr @blacklist_v6 drop; }
 chain upload { type filter hook postrouting priority filter; policy accept; }
 chain download { type filter hook prerouting priority filter; policy accept; }
}
EOF
 fi
 nft -f "$OVPN_DATA/openvpn.nft"
}
run_server() {
 [[ -c /dev/net/tun ]] || fail 'TUN device missing: map /dev/net/tun explicitly'
 # NAT applies only inside this container network namespace. Never use host mode.
 load_settings
 iptables-nft -t nat -C POSTROUTING -s "$OVPN_SUBNET" -j MASQUERADE 2>/dev/null || iptables-nft -t nat -A POSTROUTING -s "$OVPN_SUBNET" -j MASQUERADE
 if [[ "$OVPN_IPV6" == true ]];then ip6tables-nft -t nat -C POSTROUTING -s "$OVPN_SUBNET6" -j MASQUERADE 2>/dev/null || ip6tables-nft -t nat -A POSTROUTING -s "$OVPN_SUBNET6" -j MASQUERADE;fi
 exec /usr/sbin/openvpn --config "$OVPN_DATA/server.conf"
}
case "${script_type:-}" in
 client-connect)client_connect "$1";exit;;
 client-disconnect)client_disconnect;exit;;
 learn-address)if [[ "${1:-}" != delete ]];then set_firewall;fi;exit;;
esac
case "${1:-}" in
 genclient)
  identity "${2:-}";private_state
  if [[ -n "${6:-}" ]];then validate_data_config "$6";printf '%s\n' "$6" > "$OVPN_DATA/ccd/$2";fi
  genclient "$2" "${3:-}" "${4:-}" "${5:-}" "${7:-false}";;
 auth)
  [[ "${2:-}" == true ]] || fail 'disabling authentication is prohibited'
  sed -i 's/^#auth-user-pass-verify/auth-user-pass-verify/' "$OVPN_DATA/server.conf";;
 renewcert)
  if ! [[ "${2:-}" =~ ^[0-9]{1,4}$ ]]; then fail 'invalid certificate lifetime'; fi
  if ! ((10#$2>0)); then fail 'invalid certificate lifetime'; fi
  load_settings;private_state;exec 9>"$OVPN_DATA/.pki.lock";flock -x 9
  /usr/share/easy-rsa/easyrsa --batch --days="$2" renew-ca
  /usr/share/easy-rsa/easyrsa --batch --days="$2" renew "$SERVER_NAME"
  /usr/share/easy-rsa/easyrsa --batch revoke-renewed "$SERVER_NAME"
  /usr/share/easy-rsa/easyrsa --batch --days="$2" gen-crl
  private_state;;
 /usr/sbin/openvpn)
  private_state
  [[ -f "$SYSTEM_CONFIG" ]] || fail 'application bootstrap must create config.json first'
  if [[ ! -f "$OVPN_DATA/server.conf" ]];then init_pki;init_config;fi
  load_nftconfig;run_server;;
 /usr/bin/supervisord)private_state;exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf;;
 *)fail 'unsupported entrypoint command';;
esac
