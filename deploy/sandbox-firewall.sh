#!/usr/bin/env bash
# Host firewall for bot computers (run as root on the Docker host).
#
# Bot computers live on the superbot_sandbox network and have no root, so
# they cannot be firewalled from inside. These rules, on the host, keep a
# computer to the public internet:
#
#   * A computer may not open connections to private, loopback, link-local,
#     carrier-grade NAT or multicast addresses: not to the other services on
#     the sandbox network (the API's port 8080), not to another bot's
#     computer, not to the host's LAN or a cloud metadata service
#     (169.254.169.254, 100.100.100.200).
#   * A computer may not open connections to the host itself (SSH, anything
#     listening on the host's addresses).
#   * Allowed: replies to connections others opened, DNS, the public
#     internet, and connections to the computers' gate ports (9223 CDP,
#     6080 noVNC). The worker and the API reach the computers there; the gate
#     itself requires each computer's own secret (deploy/images/computer/
#     gate.py), so another computer gets nothing from it.
#
# The rules only match traffic arriving from the sandbox bridge, so the other
# Compose services are unaffected. The API and the worker are also on that
# network; docker-compose.yml gives their default network the gateway
# priority, so their own outbound traffic (database, model providers,
# host.docker.internal) never leaves through the sandbox bridge.
#
# Usage: deploy/sandbox-firewall.sh apply|remove|status
#   apply   create or refresh the rules (idempotent; safe to run repeatedly)
#   remove  delete them again
#   status  show them with packet counters
#
# Rules are not persistent: run `apply` at boot after Docker started (see
# deploy/systemd/superbot-sandbox-firewall.service) and after the sandbox
# network is recreated. deploy.sh refreshes them on every deploy once they
# have been applied.
#
# Environment: SUPERBOT_SANDBOX_NETWORK (default superbot_sandbox);
# SUPERBOT_FIREWALL_BRIDGE names the bridge directly (testing).
set -euo pipefail

NETWORK="${SUPERBOT_SANDBOX_NETWORK:-superbot_sandbox}"
FWD=SUPERBOT-SANDBOX-FWD
IN=SUPERBOT-SANDBOX-IN
GATE_PORTS=9223,6080
PRIVATE_V4=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,169.254.0.0/16,100.64.0.0/10,127.0.0.0/8,0.0.0.0/8,198.18.0.0/15,224.0.0.0/4,240.0.0.0/4
PRIVATE_V6=fc00::/7,fe80::/10,::1/128,ff00::/8

bridge_name() {
  if [[ -n "${SUPERBOT_FIREWALL_BRIDGE:-}" ]]; then
    echo "$SUPERBOT_FIREWALL_BRIDGE"
    return
  fi
  local name id
  name="$(docker network inspect -f '{{index .Options "com.docker.network.bridge.name"}}' "$NETWORK" 2>/dev/null || true)"
  if [[ -n "$name" && "$name" != "<no value>" ]]; then
    echo "$name"
    return
  fi
  id="$(docker network inspect -f '{{.Id}}' "$NETWORK" 2>/dev/null || true)"
  if [[ -z "$id" ]]; then
    echo "network $NETWORK not found; start the stack first" >&2
    exit 1
  fi
  echo "br-${id:0:12}"
}

ipv6_enabled() {
  [[ -z "${SUPERBOT_FIREWALL_BRIDGE:-}" ]] &&
    [[ "$(docker network inspect -f '{{.EnableIPv6}}' "$NETWORK" 2>/dev/null || echo false)" == "true" ]]
}

# ensure_chain TOOL CHAIN: create (or empty) a chain in the filter table.
ensure_chain() {
  "$1" -w -N "$2" 2>/dev/null || "$1" -w -F "$2"
}

# ensure_jump TOOL PARENT ARGS...: insert a rule at the top of PARENT once.
ensure_jump() {
  local tool=$1 parent=$2
  shift 2
  # iptables-nft prints the matched rule on a successful -C; only the exit
  # status matters here.
  "$tool" -w -C "$parent" "$@" >/dev/null 2>&1 || "$tool" -w -I "$parent" 1 "$@"
}

forward_parent() {
  # Docker's iptables backend sends forwarded traffic through DOCKER-USER
  # first. Without it (nftables backend) the rules hook into FORWARD itself;
  # a reject there stops the packet whatever Docker's own tables accept.
  if "$1" -w -C FORWARD -j DOCKER-USER >/dev/null 2>&1; then
    echo DOCKER-USER
  else
    echo FORWARD
  fi
}

apply_family() {
  local tool=$1 private=$2 br=$3 reject=$4
  local parent
  parent="$(forward_parent "$tool")"
  ensure_chain "$tool" "$FWD"
  "$tool" -w -A "$FWD" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
  "$tool" -w -A "$FWD" -o "$br" -p tcp -m multiport --dports "$GATE_PORTS" -j RETURN
  "$tool" -w -A "$FWD" -p udp --dport 53 -j RETURN
  "$tool" -w -A "$FWD" -p tcp --dport 53 -j RETURN
  "$tool" -w -A "$FWD" -d "$private" -p tcp -j REJECT --reject-with tcp-reset
  "$tool" -w -A "$FWD" -d "$private" -j REJECT --reject-with "$reject"
  "$tool" -w -A "$FWD" -j RETURN
  ensure_jump "$tool" "$parent" -i "$br" -j "$FWD"

  ensure_chain "$tool" "$IN"
  "$tool" -w -A "$IN" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
  "$tool" -w -A "$IN" -p udp --dport 53 -j RETURN
  "$tool" -w -A "$IN" -p tcp --dport 53 -j RETURN
  "$tool" -w -A "$IN" -p tcp -j REJECT --reject-with tcp-reset
  "$tool" -w -A "$IN" -j REJECT --reject-with "$reject"
  ensure_jump "$tool" INPUT -i "$br" -j "$IN"
}

remove_family() {
  local tool=$1 br=$2
  while "$tool" -w -D DOCKER-USER -i "$br" -j "$FWD" 2>/dev/null; do :; done
  while "$tool" -w -D FORWARD -i "$br" -j "$FWD" 2>/dev/null; do :; done
  while "$tool" -w -D INPUT -i "$br" -j "$IN" 2>/dev/null; do :; done
  for chain in "$FWD" "$IN"; do
    "$tool" -w -F "$chain" 2>/dev/null || true
    "$tool" -w -X "$chain" 2>/dev/null || true
  done
}

if [[ $EUID -ne 0 ]]; then
  echo "run as root" >&2
  exit 1
fi
command -v iptables >/dev/null || { echo "iptables not found" >&2; exit 1; }

action="${1:-status}"
br="$(bridge_name)"
case "$action" in
  apply)
    if [[ "$(sysctl -n net.bridge.bridge-nf-call-iptables 2>/dev/null || echo 0)" != "1" ]]; then
      echo "warning: net.bridge.bridge-nf-call-iptables is not 1, so traffic between containers on" >&2
      echo "         the same network bypasses these rules; run: modprobe br_netfilter &&" >&2
      echo "         sysctl -w net.bridge.bridge-nf-call-iptables=1" >&2
    fi
    apply_family iptables "$PRIVATE_V4" "$br" icmp-admin-prohibited
    if ipv6_enabled && command -v ip6tables >/dev/null; then
      apply_family ip6tables "$PRIVATE_V6" "$br" icmp6-adm-prohibited
    fi
    echo "sandbox firewall applied on $br ($NETWORK)"
    ;;
  remove)
    remove_family iptables "$br"
    command -v ip6tables >/dev/null && remove_family ip6tables "$br"
    echo "sandbox firewall removed from $br ($NETWORK)"
    ;;
  status)
    echo "bridge: $br ($NETWORK)"
    iptables -w -L "$FWD" -n -v 2>/dev/null || echo "not applied"
    iptables -w -L "$IN" -n -v 2>/dev/null || true
    ;;
  *)
    echo "usage: $0 apply|remove|status" >&2
    exit 2
    ;;
esac
