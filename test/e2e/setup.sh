#!/usr/bin/env bash
# Creates a kind cluster with kgateway, the fixtures and the UI, then
# port-forwards the UI to 127.0.0.1:18080. Usage:
#   KGW_VERSION=v2.4.7 GWAPI_VERSION=v1.6.1 [SECOND_INSTALL=1] [OBSERVE_XLISTENERSET=1] [EXPLORER=0] [UI_EXTRA_ARGS=...] test/e2e/setup.sh
#   test/e2e/setup.sh down    # stop the port-forward and delete the cluster
# EXPLORER=0 skips fixtures/explorer.yaml (the D-021 observations need a cluster without it).
# UI_EXTRA_ARGS are space-separated UI flags for the chart's extraArgs, e.g. --poll-interval=10m;
# a value must not contain a comma (helm's --set list syntax splits on it).
# The cluster's kubeconfig is written to $E2E_DIR/kubeconfig; the current
# kubeconfig context is left alone.
set -euo pipefail
cd "$(dirname "$0")/../.."

CLUSTER=${CLUSTER:-kgwui-e2e}
tmp=${TMPDIR:-/tmp}
E2E_DIR=${E2E_DIR:-${tmp%/}/$CLUSTER}
PORT=${PORT:-18080}
export KUBECONFIG=$E2E_DIR/kubeconfig

if [[ "$CLUSTER" == kgwui-dev ]]; then
  echo "refusing to manage the development cluster kgwui-dev" >&2
  exit 1
fi

down() {
  if [[ -f "$E2E_DIR/port-forward.pid" ]]; then
    kill "$(cat "$E2E_DIR/port-forward.pid")" 2>/dev/null || true
    rm -f "$E2E_DIR/port-forward.pid"
  fi
  if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
    kind delete cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG"
  fi
}

# poll runs "$@" every second until it succeeds or $1 seconds pass.
poll() {
  local timeout=$1; shift
  local deadline=$((SECONDS + timeout))
  until "$@" >/dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      echo "timed out after ${timeout}s: $*" >&2
      return 1
    fi
    sleep 1
  done
}

if [[ "${1:-}" == down ]]; then
  down
  exit 0
fi

: "${KGW_VERSION:?}" "${GWAPI_VERSION:?}"
mkdir -p "$E2E_DIR"
down # idempotent: start from a fresh cluster
kind create cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG" --wait 120s
# The experimental channel adds TCPRoute, TLSRoute and ListenerSet for the computed-view e2e.
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/${GWAPI_VERSION}/experimental-install.yaml"
if [[ "${OBSERVE_XLISTENERSET:-}" == 1 ]]; then
  # Legacy XListenerSet for the D-021 observations only; v1.4.1 is the last release that ships it.
  kubectl apply --server-side -f "https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/v1.4.1/config/crd/experimental/gateway.networking.x-k8s.io_xlistenersets.yaml"
fi
helm install kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds --version "$KGW_VERSION" -n kgw-a --create-namespace
# Release "kgateway" in each namespace: the controller Deployment is <ns>/kgateway (kgateway.fullname).
helm install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version "$KGW_VERSION" -n kgw-a \
  --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"a"}}]' --wait
kubectl -n kgw-a rollout status deploy/kgateway --timeout=180s # fails fast if the rendered name changes
kubectl apply -f test/e2e/fixtures/base.yaml
if [[ "${EXPLORER:-1}" == 1 ]]; then
  # Self-signed certificate for edge's https listener; the key never outlives this block.
  d=$(mktemp -d)
  openssl req -x509 -newkey rsa:2048 -nodes -days 30 -subj "/CN=*.example.com" \
    -keyout "$d/tls.key" -out "$d/tls.crt" >/dev/null 2>&1
  kubectl -n infra create secret tls edge-tls --cert "$d/tls.crt" --key "$d/tls.key" >/dev/null
  rm -rf "$d"
  kubectl apply -f test/e2e/fixtures/explorer.yaml
fi
if [[ "${SECOND_INSTALL:-}" == 1 ]]; then
  helm install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version "$KGW_VERSION" -n kgw-b --create-namespace \
    --set-json 'discoveryNamespaceSelectors=[{"matchLabels":{"team":"b"}}]' --wait
  kubectl -n kgw-b rollout status deploy/kgateway --timeout=180s
  kubectl apply -f test/e2e/fixtures/second-install.yaml
fi
# The Gateway must exist before kubectl wait can watch it.
poll 60 kubectl -n infra get gateway/edge
kubectl -n infra wait --for=condition=Programmed gateway/edge --timeout=180s
kubectl -n shop rollout status deploy/httpbin --timeout=180s

docker build -t kgateway-ui:e2e --build-arg VERSION=e2e .
kind load docker-image kgateway-ui:e2e --name "$CLUSTER"
ui_args=()
if [[ -n "${UI_EXTRA_ARGS:-}" ]]; then
  read -ra extra <<<"$UI_EXTRA_ARGS"
  ui_args=(--set "extraArgs={$(IFS=,; echo "${extra[*]}")}")
fi
# ${a[@]+...} keeps an empty array safe under set -u on bash 3.
helm install ui deploy/helm/kgateway-ui -n kgateway-ui --create-namespace \
  --set image.repository=kgateway-ui --set image.tag=e2e --set image.pullPolicy=Never ${ui_args[@]+"${ui_args[@]}"} --wait
kubectl -n kgateway-ui rollout status deploy/ui-kgateway-ui --timeout=120s

nohup kubectl -n kgateway-ui port-forward svc/ui-kgateway-ui "$PORT:80" >"$E2E_DIR/port-forward.log" 2>&1 &
echo $! >"$E2E_DIR/port-forward.pid"
if ! poll 60 curl -fsS "http://127.0.0.1:$PORT/readyz"; then
  cat "$E2E_DIR/port-forward.log" >&2
  exit 1
fi
echo "UI at http://127.0.0.1:$PORT (kubeconfig: $KUBECONFIG)"
