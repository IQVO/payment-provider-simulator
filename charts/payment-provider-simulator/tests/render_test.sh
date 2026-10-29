#!/usr/bin/env bash
# Chart render assertions for payment-provider-simulator: Postgres is the only
# runtime (no Kafka, no consumer - ADR 0002 A16), so the mandatory configuration
# has no silent default, no *_MODE and no *_CONSUMER_GROUP is ever rendered, the
# pod is hardened and has room to shut down, and the env cmd/api reads is the
# env the chart renders.
# Run: bash charts/payment-provider-simulator/tests/render_test.sh   (needs helm)
set -euo pipefail

chart_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_dir="$(cd "$chart_dir/../.." && pwd)"
release=payment-provider-simulator
# Dummy values, no password: the chart refuses to render without a database
# source. Nothing here is ever dialled.
base=(--set database.url=postgres://u@example.invalid:5432/db)
# Every optional value set, so every env var cmd/api can read is rendered.
everything=(--set config.providerTimeoutDelay=1500ms --set config.shutdownDrainDelay=5s
  --set database.migrationsUrl=postgres://u@example.invalid:5432/direct
  --set autoscaling.api.enabled=true --set ingress.enabled=true --set gatewayApi.enabled=true)

fail() { echo "FAIL: $*" >&2; exit 1; }
command -v helm >/dev/null || fail "helm is not installed"

configmap="$(helm template "$release" "$chart_dir" "${base[@]}" --show-only templates/configmap.yaml)"
api_deployment="$(helm template "$release" "$chart_dir" "${base[@]}" --show-only templates/deployment.yaml)"
configmap_name="$(awk '/^  name:/ {print $2; exit}' <<<"$configmap")"
[[ -n "$configmap_name" ]] || fail "could not read the ConfigMap name"

# Every non-secret config value is rendered into the chart ConfigMap, and the api
# Deployment -- the only workload -- must mount it with envFrom, or a value that
# is only in the ConfigMap silently does nothing.
grep -A3 'envFrom:' <<<"$api_deployment" | grep -q 'configMapRef:' \
  || fail "api Deployment does not mount the ConfigMap via envFrom"
grep -A3 'envFrom:' <<<"$api_deployment" | grep -q "name: $configmap_name" \
  || fail "api Deployment envFrom does not reference ConfigMap $configmap_name"
grep -q 'checksum/config:' <<<"$api_deployment" || fail "the pods must roll when the ConfigMap changes (checksum/config)"

# A tuned config.* value reaches the ConfigMap the api mounts and the container env.
tuned="$(helm template "$release" "$chart_dir" "${base[@]}" --set config.providerTimeoutDelay=2s \
  --set config.shutdownDrainDelay=7s --show-only templates/configmap.yaml)"
grep -q 'PROVIDER_TIMEOUT_DELAY: "2s"' <<<"$tuned" || fail "config.providerTimeoutDelay is not rendered into the ConfigMap"
grep -q 'SHUTDOWN_DRAIN_DELAY: "7s"' <<<"$tuned" || fail "config.shutdownDrainDelay is not rendered into the ConfigMap"

# shutdownDrainDelay=0 (no drain wait) is rendered as is.
zero_drain="$(helm template "$release" "$chart_dir" "${base[@]}" --set config.shutdownDrainDelay=0 --show-only templates/configmap.yaml)"
grep -q 'SHUTDOWN_DRAIN_DELAY: "0"' <<<"$zero_drain" || fail "config.shutdownDrainDelay=0 is not rendered"

# Chart vs binary: every env var cmd/api reads must be rendered into the
# Deployment when every optional value is set. The names come from the binary's
# own os.Getenv / getenv("...") / env("...") literals, so a new variable in
# cmd/api fails this test until the chart renders it.
binary_env() {
  grep -ohE '(Getenv|getenv|env)\("[A-Z][A-Z0-9_]*"' "$repo_dir"/cmd/api/*.go --exclude='*_test.go' \
    | sed -E 's/.*\("//; s/"$//' | sort -u
}
# Word splitting is safe: the names are [A-Z0-9_] only (bash 3.2 has no mapfile).
# shellcheck disable=SC2207
api_vars=($(binary_env))
(( ${#api_vars[@]} >= 6 )) || fail "found only ${#api_vars[@]} env names in cmd/api; the scan is looking in the wrong place"
full_deployment="$(helm template "$release" "$chart_dir" "${base[@]}" "${everything[@]}" --show-only templates/deployment.yaml)"
for var in "${api_vars[@]}" DATABASE_URL MIGRATIONS_DATABASE_URL \
  HTTP_ADDR LOG_LEVEL PROVIDER_TIMEOUT_DELAY SHUTDOWN_DRAIN_DELAY; do
  grep -q "name: $var\$" <<<"$full_deployment" || fail "the api Deployment does not render $var, which cmd/api reads"
done

# Every key the ConfigMap renders is a variable cmd/api reads (no dead keys), and
# the ConfigMap never carries a DSN.
full_configmap="$(helm template "$release" "$chart_dir" "${base[@]}" "${everything[@]}" --show-only templates/configmap.yaml)"
api_known=" ${api_vars[*]} "
while read -r key; do
  [[ "$api_known" == *" $key "* ]] || fail "ConfigMap key $key is read by no binary"
done < <(awk '/^data:/ {d=1; next} d && /^  [A-Z]/ {sub(/:.*/, ""); sub(/^  /, ""); print}' <<<"$full_configmap")
if grep -qE '^  (MIGRATIONS_)?DATABASE_URL:' <<<"$full_configmap"; then fail "a DSN must never land in the ConfigMap"; fi

# No Kafka: the simulator publishes and consumes no events, so the render must
# never carry a brokers variable or a consumer group id.
render_all="$(helm template "$release" "$chart_dir" "${base[@]}" "${everything[@]}")"
if grep -q 'name: KAFKA_BROKERS$' <<<"$render_all"; then fail "the render carries KAFKA_BROKERS; this service has no Kafka"; fi
if grep -qE 'name: [A-Z_]*_CONSUMER_GROUP$' <<<"$render_all"; then fail "the render carries a consumer group id; this service runs no consumer"; fi
if grep -qE '^  (KAFKA_BROKERS|[A-Z_]*_CONSUMER_GROUP):' <<<"$full_configmap"; then fail "the ConfigMap carries a Kafka or consumer-group key"; fi

# DATABASE_URL comes from a Secret; MIGRATIONS_DATABASE_URL is optional.
grep -A4 'name: DATABASE_URL$' <<<"$api_deployment" | grep -q 'secretKeyRef:' || fail "DATABASE_URL must come from a Secret (secretKeyRef)"
grep -A6 'name: MIGRATIONS_DATABASE_URL$' <<<"$full_deployment" | grep -q 'secretKeyRef:' || fail "MIGRATIONS_DATABASE_URL must come from a Secret (secretKeyRef)"
grep -A6 'name: MIGRATIONS_DATABASE_URL$' <<<"$full_deployment" | grep -q 'optional: true' || fail "MIGRATIONS_DATABASE_URL must be optional: true"
grep -A6 'name: MIGRATIONS_DATABASE_URL$' <<<"$full_deployment" | grep -q 'key: MIGRATIONS_DATABASE_URL' \
  || fail "MIGRATIONS_DATABASE_URL must read database.migrationsExistingSecretKey"
existing="$(helm template "$release" "$chart_dir" --set database.existingSecret=my-db --show-only templates/deployment.yaml)"
grep -A4 'name: DATABASE_URL$' <<<"$existing" | grep -q 'name: my-db' || fail "database.existingSecret must be the DATABASE_URL Secret"
no_secret="$(helm template "$release" "$chart_dir" --set database.existingSecret=my-db)"
if grep -q 'kind: Secret' <<<"$no_secret"; then fail "no Secret may be created when database.existingSecret is set"; fi
grep -q 'kind: Secret' <<<"$render_all" || fail "database.url must create the Secret"

# Pod hardening, probes, the image tag and shutdown room.
for want in 'runAsNonRoot: true' 'readOnlyRootFilesystem: true' 'allowPrivilegeEscalation: false' 'type: RuntimeDefault' 'path: /healthz' 'path: /readyz'; do
  grep -q -- "$want" <<<"$api_deployment" || fail "the api Deployment lacks '$want'"
done
grep -A2 'drop:' <<<"$api_deployment" | grep -q 'ALL' || fail "the api container must drop ALL capabilities"
grep -A3 'livenessProbe:' <<<"$api_deployment" | grep -q '/healthz' || fail "livenessProbe must be /healthz"
grep -A3 'readinessProbe:' <<<"$api_deployment" | grep -q '/readyz' || fail "readinessProbe must be /readyz"
grep -q 'image: "warehouse/payment-provider-simulator:v0.1.0"' <<<"$api_deployment" \
  || fail "an empty image.tag must resolve to v<appVersion>, the tag the release job publishes"
grace="$(awk '/terminationGracePeriodSeconds:/ {print $2; exit}' <<<"$api_deployment")"
# SHUTDOWN_DRAIN_DELAY 5s + HTTP drain 10s + 10s buffer = 25s (cmd/api/main.go).
(( grace > 25 )) || fail "terminationGracePeriodSeconds is $grace; it must exceed the binary's shutdown bounds (25s)"

# No *_MODE or in-memory/permissive wording anywhere in the render.
if grep -qE '(^|[^A-Z_])[A-Z]+_MODE\b' <<<"$render_all"; then fail "the render carries a *_MODE variable, which this service does not have"; fi
if grep -qiE 'in-memory|silently runs|permissive' <<<"$render_all"$'\n'"$(cat "$chart_dir/templates/NOTES.txt")"; then
  fail "the render or NOTES.txt mentions an in-memory / permissive mode"
fi

# Only the api component exists: one Deployment, one Service, no MCP or frontend.
for stray in mcp frontend projector reports analytics; do
  if grep -qE "app.kubernetes.io/component: .*$stray" <<<"$render_all"; then fail "the chart must not render a $stray component"; fi
done
[[ "$(grep -c '^kind: Deployment' <<<"$render_all")" == 1 ]] || fail "exactly one Deployment (the api) must render"
[[ "$(grep -c '^kind: Service$' <<<"$render_all")" == 1 ]] || fail "exactly one Service (the api) must render"
for off in HorizontalPodAutoscaler Ingress HTTPRoute; do
  if grep -q "^kind: $off" <<<"$(helm template "$release" "$chart_dir" "${base[@]}")"; then fail "$off must be off by default"; fi
  grep -q "^kind: $off" <<<"$render_all" || fail "$off must render when enabled"
done

# --- refusals: the mandatory values name themselves ---------------------------------
expect_refusal() { # expect_refusal <why> <needle> <helm args...>
  local why="$1" needle="$2" err; shift 2
  if err="$(helm template "$release" "$chart_dir" "$@" 2>&1)"; then
    fail "the chart must refuse to render: $why"
  fi
  grep -qF -- "$needle" <<<"$err" || fail "the refusal for '$why' does not name '$needle':
$err"
}
expect_refusal "no database source" "database.url or database.existingSecret"
expect_refusal "a *_MODE through extraEnv" "_MODE" "${base[@]}" --set 'extraEnv[0].name=CACHE_MODE' --set 'extraEnv[0].value=permissive'
expect_refusal "a *_MODE through extraEnv (off)" "_MODE" "${base[@]}" --set 'extraEnv[0].name=PROVIDER_MODE' --set 'extraEnv[0].value=off'
expect_refusal "a consumer group through extraEnv" "_CONSUMER_GROUP" "${base[@]}" --set 'extraEnv[0].name=PAYMENTS_CONSUMER_GROUP' --set 'extraEnv[0].value=x'
expect_refusal "a chart-owned variable through extraEnv" "DATABASE_URL" "${base[@]}" --set 'extraEnv[0].name=DATABASE_URL' --set 'extraEnv[0].value=x'
expect_refusal "a grace period under the shutdown bounds" "terminationGracePeriodSeconds" "${base[@]}" --set terminationGracePeriodSeconds=20
expect_refusal "a grace period equal to the shutdown bounds" "must exceed 25" "${base[@]}" --set terminationGracePeriodSeconds=25
expect_refusal "no grace period" "terminationGracePeriodSeconds" "${base[@]}" --set terminationGracePeriodSeconds=0
helm template "$release" "$chart_dir" "${base[@]}" --set terminationGracePeriodSeconds=26 >/dev/null \
  || fail "a 26s grace period (just above the 25s bounds) must still render"
# A longer drain delay raises the bar by the same amount.
expect_refusal "a 30s drain delay with the default grace period" "must exceed 50" "${base[@]}" --set config.shutdownDrainDelay=30s
helm template "$release" "$chart_dir" "${base[@]}" --set config.shutdownDrainDelay=30s --set terminationGracePeriodSeconds=51 >/dev/null \
  || fail "a 30s drain delay with a 51s grace period must render"
expect_refusal "a drain delay the chart cannot size" "config.shutdownDrainDelay" "${base[@]}" --set config.shutdownDrainDelay=soon
expect_refusal "an http address off the container port" "config.httpAddr" "${base[@]}" --set config.httpAddr=:9090

# The full render (ct lint's all-components values file) carries every optional
# template together, still exactly one Deployment and one Service.
together="$(helm template "$release" "$chart_dir" -f "$chart_dir/ci/all-components-values.yaml")"
grep -q 'app.kubernetes.io/component: api$' <<<"$together" || fail "the full render lacks the api component"
[[ "$(grep -c '^kind: Deployment' <<<"$together")" == 1 ]] || fail "the full render must carry exactly the api Deployment"
[[ "$(grep -c '^kind: Service$' <<<"$together")" == 1 ]] || fail "the full render must carry exactly the api Service"

echo "PASS: payment-provider-simulator chart render assertions"
