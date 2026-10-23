#!/usr/bin/env bash
# Runs the D-021 observations (computed-rules.md §10) on a cluster made by test/e2e/setup.sh.
#   run.sh prepare          # TLS Secret for listener https, then 00-world.yaml
#   run.sh apply FILE...    # apply, wait until current and settled, print the record as JSON
#   run.sh settle           # wait as apply does, then print the record
#   run.sh reset            # delete every object labelled observe=d021
#   run.sh xds PREFIX       # proxy routes whose path match starts with PREFIX
set -euo pipefail
cd "$(dirname "$0")"

CLUSTER=${CLUSTER:-kgwui-e2e}
tmp=${TMPDIR:-/tmp}
export KUBECONFIG=${KUBECONFIG:-${E2E_DIR:-${tmp%/}/$CLUSTER}/kubeconfig}
KINDS=httproute,listenerset
kubectl get crd xlistenersets.gateway.networking.x-k8s.io >/dev/null 2>&1 && KINDS+=,xlistenerset

# current: the Gateway and shop/api (when present) report on their current generation.
current() {
  kubectl -n infra get gateway edge -o json |
    jq -e '.metadata.generation as $g | (.status.conditions // []) | length > 0 and all(.observedGeneration == $g)' >/dev/null || return 1
  local api
  api=$(kubectl -n shop get httproute api -o json --ignore-not-found)
  [[ -z "$api" ]] && return 0
  jq -e '.metadata.generation as $g | [.status.parents[]?.conditions[]?] | length > 0 and all(.observedGeneration == $g)' <<<"$api" >/dev/null
}

record() {
  jq -n \
    --argjson routes "$(kubectl get httproute -A -l observe=d021 -o json |
      jq '[.items[] | {n: "\(.metadata.namespace)/\(.metadata.name)", gen: .metadata.generation, parents: .status.parents}]')" \
    --argjson gateway "$(kubectl -n infra get gateway edge -o json |
      jq '{gen: .metadata.generation, listeners: [.status.listeners[]? | {name, attachedRoutes}]}')" \
    --argjson sets "$(kubectl get "${KINDS#httproute,}" -A -o json |
      jq '[.items[] | {kind, n: "\(.metadata.namespace)/\(.metadata.name)", gen: .metadata.generation, conditions: [.status.conditions[]? | {type, status, reason, observedGeneration}], listeners: [.status.listeners[]? | {name, attachedRoutes}]}]')" \
    '{routes: $routes, gateway: $gateway, listenerSets: $sets}'
}

# settle waits until the Gateway and parent are current, then until two records 5 s apart match.
settle() {
  local deadline=$((SECONDS + 120)) prev="" cur
  until current; do
    ((SECONDS < deadline)) || { echo "not current after 120s" >&2; return 1; }
    sleep 2
  done
  sleep 5
  cur=$(record)
  until [[ "$cur" == "$prev" ]]; do
    ((SECONDS < deadline)) || { echo "status still changing after 120s" >&2; return 1; }
    prev=$cur
    sleep 5
    cur=$(record)
  done
  echo "$cur"
}

case "${1:-}" in
prepare)
  if ! kubectl -n infra get secret edge-tls >/dev/null 2>&1; then
    d=$(mktemp -d)
    trap 'rm -rf "$d"' EXIT # the key never outlives the script, even on failure
    openssl req -x509 -newkey rsa:2048 -nodes -days 30 -subj "/CN=*.example.com" \
      -keyout "$d/tls.key" -out "$d/tls.crt" >/dev/null 2>&1
    kubectl -n infra create secret tls edge-tls --cert "$d/tls.crt" --key "$d/tls.key" >/dev/null
  fi
  kubectl apply -f 00-world.yaml
  kubectl -n infra wait --for=condition=Programmed gateway/edge --timeout=180s
  ;;
apply)
  shift
  for f in "$@"; do kubectl apply -f "$f" >&2; done
  settle
  ;;
settle)
  settle
  ;;
reset)
  kubectl delete "$KINDS" -A -l observe=d021 --wait >&2
  ;;
xds)
  kubectl -n infra port-forward deploy/edge 19100:19000 >/dev/null 2>&1 &
  pf=$!
  trap 'kill $pf 2>/dev/null' EXIT
  for _ in $(seq 20); do curl -fsS -o /dev/null http://127.0.0.1:19100/ready 2>/dev/null && break; sleep 1; done
  curl -fsS "http://127.0.0.1:19100/config_dump?resource=dynamic_route_configs" |
    jq --arg p "$2" '[.. | objects | select(has("match") and has("name")) | {name, match} | select(((.match.prefix // .match.path // .match.path_separated_prefix // "") | startswith($p)))]'
  ;;
*)
  sed -n '2,7p' "$0" >&2
  exit 2
  ;;
esac
