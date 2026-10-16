#!/usr/bin/env bash
# End-to-end test of the Cilium CNI addon (spec.addons.cni / spec.addons.cilium)
# on two kind clusters, without Xen Orchestra:
#
#   - "management": Cluster API + the vates provider built from this checkout,
#     with fake XO credentials (the CNI addon never talks to XO);
#   - "workload": a kind cluster without CNI (and without kube-proxy in
#     kube-proxy replacement mode), standing in for the cluster the provider
#     would have created on Xen Orchestra.
#
# A Cluster + XOCluster pair is created on the management cluster with a
# hand-made kubeconfig Secret pointing at the workload cluster. The provider
# renders the Cilium manifest into a ClusterResourceSet and Cluster API applies
# it to the workload cluster, exactly as for a real cluster. The test then
# checks the rendered manifest, the API validation, and that the workload
# cluster gets Ready nodes and working Services.
#
# Usage: hack/e2e-cilium.sh
#   MODE=kube-proxy-replacement|standard   (default: kube-proxy-replacement)
#   CONTROL_PLANE=talos|kubeadm            (default: talos) - kind of the
#       Cluster controlPlaneRef: "talos" renders the Talos values of the chart
#   KEEP_CLUSTERS=true                     keep the kind clusters at the end
#   CONTAINER_TOOL=docker|podman           (default: docker; with podman, also
#       export KIND_EXPERIMENTAL_PROVIDER=podman)
#
# Requires: docker or podman, kind, kubectl, clusterctl.
#
# Talos KubePrism (localhost:7445) does not exist on kind nodes: in kube-proxy
# replacement mode the workload cluster sets an explicit k8sServiceHost /
# k8sServicePort (its control plane), and the KubePrism default is checked on
# the rendered manifest of a second, render-only cluster.
set -euo pipefail

MODE=${MODE:-kube-proxy-replacement}
CONTROL_PLANE=${CONTROL_PLANE:-talos}
MGMT=${MGMT_CLUSTER:-vates-cilium-mgmt}
WL=${WORKLOAD_CLUSTER:-vates-cilium-wl}
IMG=${IMG:-ghcr.io/vatesfr/cluster-api-provider-vates:e2e}
NS=default
TIMEOUT=${TIMEOUT:-600}
CONTAINER_TOOL=${CONTAINER_TOOL:-docker}

case "$MODE" in
kube-proxy-replacement) KPR=true ;;
standard) KPR=false ;;
*) echo "unknown MODE $MODE" >&2; exit 2 ;;
esac
case "$CONTROL_PLANE" in
talos) CP_GROUP=controlplane.cluster.x-k8s.io CP_KIND=TalosControlPlane ;;
kubeadm) CP_GROUP=controlplane.cluster.x-k8s.io CP_KIND=KubeadmControlPlane ;;
*) echo "unknown CONTROL_PLANE $CONTROL_PLANE" >&2; exit 2 ;;
esac

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
MGMT_KUBECONFIG=$WORK/mgmt.kubeconfig
WL_KUBECONFIG=$WORK/wl.kubeconfig

log() { printf '\n=== %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
km() { kubectl --kubeconfig "$MGMT_KUBECONFIG" "$@"; }
kw() { kubectl --kubeconfig "$WL_KUBECONFIG" "$@"; }

# retry <seconds> <command...>: retries the command every 5s until it succeeds.
retry() {
	local deadline=$((SECONDS + $1))
	shift
	until "$@"; do
		((SECONDS < deadline)) || return 1
		sleep 5
	done
}

dump() {
	local rc=$?
	if [ "$rc" -ne 0 ]; then
		log "Debug information (exit code $rc)"
		km get cluster,xocluster,clusterresourceset,clusterresourcesetbinding -A -o wide || true
		km -n "$NS" get xocluster -o yaml || true
		km -n "$NS" get clusterresourcesetbinding -o yaml || true
		km -n capi-system logs deploy/vates-capi-controller-manager -c manager --tail=200 || true
		km -n capi-system logs deploy/capi-controller-manager --tail=100 || true
		kw get nodes -o wide || true
		kw -n kube-system get pods -o wide || true
		kw -n kube-system describe pods -l k8s-app=cilium || true
		kw -n kube-system logs ds/cilium -c cilium-agent --tail=100 || true
	fi
	if [ "${KEEP_CLUSTERS:-false}" != true ]; then
		kind delete cluster --name "$MGMT" >/dev/null 2>&1 || true
		kind delete cluster --name "$WL" >/dev/null 2>&1 || true
	fi
	rm -rf "$WORK"
}
trap dump EXIT

log "Creating the kind clusters (mode=$MODE, control plane=$CONTROL_PLANE)"
if [ "$KPR" = true ]; then PROXY_MODE=none; else PROXY_MODE=iptables; fi
cat >"$WORK/wl-kind.yaml" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: true
  kubeProxyMode: $PROXY_MODE
nodes:
  - role: control-plane
  - role: worker
EOF
kind create cluster --name "$MGMT" --kubeconfig "$MGMT_KUBECONFIG"
kind create cluster --name "$WL" --config "$WORK/wl-kind.yaml" --kubeconfig "$WL_KUBECONFIG"

log "Building and loading the provider image $IMG"
"$CONTAINER_TOOL" build -t "$IMG" "$ROOT"
"$CONTAINER_TOOL" save -o "$WORK/image.tar" "$IMG"
kind load image-archive "$WORK/image.tar" --name "$MGMT"

log "Installing Cluster API (core, kubeadm and Talos providers)"
KUBECONFIG=$MGMT_KUBECONFIG clusterctl init \
	--bootstrap kubeadm,talos --control-plane kubeadm,talos --wait-providers

log "Deploying the vates provider"
km -n capi-system create secret generic xo-credentials \
	--from-literal=url=https://xo.invalid --from-literal=token=fake --from-literal=insecure=true
km kustomize "$ROOT/config/default" |
	sed "s|image: ghcr.io/vatesfr/cluster-api-provider-vates:latest|image: $IMG|" |
	km apply --server-side -f -
km -n capi-system rollout status deploy/vates-capi-controller-manager --timeout=300s

log "Checking the API validation"
reject() {
	local expected=$1 addons=$2 out
	if out=$(km apply --dry-run=server -f - 2>&1 <<EOF
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: invalid
  namespace: $NS
spec:
  addons: $addons
EOF
	); then
		fail "addons $addons was accepted"
	fi
	grep -q "$expected" <<<"$out" || fail "addons $addons: expected '$expected', got: $out"
	echo "rejected as expected: $addons"
}
reject "requires cni to be cilium" '{cni: none, cilium: {kubeProxyReplacement: true}}'
reject "requires cni to be cilium" '{cilium: {kubeProxyReplacement: true}}'
reject "must be set together" '{cni: cilium, cilium: {kubeProxyReplacement: true, k8sServiceHost: localhost}}'
reject "require kubeProxyReplacement" '{cni: cilium, cilium: {k8sServiceHost: localhost, k8sServicePort: 7445}}'

# create_cluster <name> <cilium spec> <endpoint host>: Cluster + XOCluster.
# CCM and CSI are disabled: they need a real Xen Orchestra.
create_cluster() {
	km apply -f - <<EOF
apiVersion: vates.infrastructure.cluster.x-k8s.io/v1beta2
kind: XOCluster
metadata:
  name: $1
  namespace: $NS
spec:
  controlPlaneEndpoint:
    host: $3
    port: 6443
  addons:
    ccm: false
    csi: false
    cni: cilium
    cilium: $2
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: $1
  namespace: $NS
spec:
  clusterNetwork:
    pods:
      cidrBlocks: [10.244.0.0/16]
  controlPlaneRef:
    apiGroup: $CP_GROUP
    kind: $CP_KIND
    name: $1-cp
  infrastructureRef:
    apiGroup: vates.infrastructure.cluster.x-k8s.io
    kind: XOCluster
    name: $1
EOF
}

# rendered <cluster>: the Cilium manifest rendered by the provider.
rendered() { km -n "$NS" get configmap "cni-manifests-$1" -o jsonpath='{.data.cni\.yaml}'; }
has_rendered() { [ -n "$(rendered "$1" 2>/dev/null)" ]; }
expect_in() { grep -qF -- "$2" <<<"$1" || fail "rendered manifest: missing '$2'"; }
expect_not_in() { ! grep -qF -- "$2" <<<"$1" || fail "rendered manifest: unexpected '$2'"; }

log "Creating the workload Cluster on the management cluster"
WL_IP=$("$CONTAINER_TOOL" inspect -f '{{.NetworkSettings.Networks.kind.IPAddress}}' "$WL-control-plane")
if [ "$KPR" = true ]; then
	CILIUM_SPEC="{kubeProxyReplacement: true, k8sServiceHost: \"$WL_IP\", k8sServicePort: 6443}"
else
	CILIUM_SPEC="{kubeProxyReplacement: false}"
fi
create_cluster wl "$CILIUM_SPEC" "$WL_IP"

# The workload kubeconfig, as a control plane provider would store it.
kind get kubeconfig --name "$WL" | sed "s|server: https://.*|server: https://$WL_IP:6443|" >"$WORK/wl-internal.kubeconfig"
km -n "$NS" create secret generic wl-kubeconfig --type=cluster.x-k8s.io/secret \
	--from-file=value="$WORK/wl-internal.kubeconfig"
km -n "$NS" label secret wl-kubeconfig cluster.x-k8s.io/cluster-name=wl

log "Checking the rendered Cilium manifest"
retry 180 has_rendered wl || fail "the provider did not render the Cilium manifest"
M=$(rendered wl)
if [ "$KPR" = true ]; then
	expect_in "$M" 'kube-proxy-replacement: "true"'
	[ "$(grep -c "value: \"$WL_IP\"" <<<"$M")" -eq 5 ] || fail "expected KUBERNETES_SERVICE_HOST=$WL_IP in 5 containers"
else
	expect_in "$M" 'kube-proxy-replacement: "false"'
	expect_not_in "$M" KUBERNETES_SERVICE_HOST
fi
if [ "$CONTROL_PLANE" = talos ]; then
	expect_not_in "$M" SYS_MODULE
	expect_in "$M" 'ipam: "kubernetes"'
	expect_in "$M" 'cgroup-root: "/sys/fs/cgroup"'
else
	expect_in "$M" '- SYS_MODULE'
	expect_in "$M" 'ipam: "cluster-pool"'
fi
echo "rendered manifest OK"

if [ "$CONTROL_PLANE" = talos ] && [ "$KPR" = true ]; then
	log "Checking the KubePrism default on a render-only Talos cluster"
	create_cluster kubeprism "{kubeProxyReplacement: true}" 192.0.2.10
	retry 180 has_rendered kubeprism || fail "the provider did not render the Cilium manifest"
	M=$(rendered kubeprism)
	[ "$(grep -c 'value: "localhost"' <<<"$M")" -eq 5 ] || fail "expected KUBERNETES_SERVICE_HOST=localhost in 5 containers"
	[ "$(grep -c 'value: "7445"' <<<"$M")" -eq 5 ] || fail "expected KUBERNETES_SERVICE_PORT=7445 in 5 containers"
	echo "KubePrism default OK"
fi

log "Waiting for Cluster API to apply the ClusterResourceSet to the workload cluster"
cilium_applied() { kw -n kube-system get ds cilium >/dev/null 2>&1; }
retry "$TIMEOUT" cilium_applied || fail "Cilium was not applied to the workload cluster"
kw -n kube-system rollout status ds/cilium --timeout="${TIMEOUT}s"
kw -n kube-system rollout status deploy/cilium-operator --timeout="${TIMEOUT}s"
kw wait --for=condition=Ready nodes --all --timeout="${TIMEOUT}s"

log "Checking kube-proxy and the Cilium datapath"
if [ "$KPR" = true ]; then
	! kw -n kube-system get ds kube-proxy >/dev/null 2>&1 || fail "kube-proxy is deployed"
	EXPECTED_KPR=True
else
	kw -n kube-system get ds kube-proxy >/dev/null || fail "kube-proxy is missing"
	EXPECTED_KPR=False
fi
STATUS=$(kw -n kube-system exec ds/cilium -c cilium-agent -- cilium-dbg status)
echo "$STATUS" | grep -E 'KubeProxyReplacement|Kubernetes:'
grep -qE "^KubeProxyReplacement:\s+$EXPECTED_KPR" <<<"$STATUS" || fail "KubeProxyReplacement is not $EXPECTED_KPR"

log "Checking Services (ClusterIP, DNS and NodePort)"
kw wait --for=condition=Available deploy/coredns -n kube-system --timeout=300s
kw create deployment web --image=nginx:1.27 --replicas=2
kw expose deployment web --port=80 --type=NodePort
kw rollout status deploy/web --timeout=300s
# shellcheck disable=SC2016 # the loop runs in the pod
kw run curl --image=curlimages/curl:8.11.1 --restart=Never --command -- \
	sh -c 'for i in $(seq 1 30); do curl -sf -o /dev/null http://web.default.svc.cluster.local && echo OK && exit 0; sleep 2; done; exit 1'
kw wait --for=jsonpath='{.status.phase}'=Succeeded pod/curl --timeout=120s || {
	kw logs pod/curl || true
	fail "ClusterIP Service unreachable from a pod"
}
NODE_PORT=$(kw get svc web -o jsonpath='{.spec.ports[0].nodePort}')
WORKER_IP=$("$CONTAINER_TOOL" inspect -f '{{.NetworkSettings.Networks.kind.IPAddress}}' "$WL-worker")
"$CONTAINER_TOOL" exec "$WL-control-plane" curl -sf -o /dev/null --retry 10 --retry-all-errors \
	"http://$WORKER_IP:$NODE_PORT" || fail "NodePort Service unreachable"
echo "Services OK"

log "PASS (mode=$MODE, control plane=$CONTROL_PLANE)"
