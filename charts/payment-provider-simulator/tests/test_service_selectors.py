#!/usr/bin/env python3
"""Assert this chart's Service selectors isolate each component.

Why this exists (fleet-wide): `app.kubernetes.io/name` + `instance` are identical
on every pod a release creates. A Service that selects on those two alone selects
ALL of them, which turns into a wrong answer the day a second component is added.
Every Deployment here carries `app.kubernetes.io/component` in selector.matchLabels
and its pod labels, and every Service selects on it, so each Service selects
EXACTLY ONE Deployment. This test fails if that ever stops being true.

payment-provider-simulator has exactly one component: the api (REST only — it
publishes and consumes no events, ADR 0002 A16, so there is no Kafka, no outbox
relay and no consumer). There is no mcp and no frontend: the chart renders
nothing for binaries that do not exist. The checks run with every optional
template (HPA, Ingress, HTTPRoute) enabled together.

Run: python3 charts/payment-provider-simulator/tests/test_service_selectors.py
Needs: helm, PyYAML.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

CHART_DIR = Path(__file__).resolve().parents[1]
RELEASE = "payment-provider-simulator"

# Dummy values, no password: the chart refuses to render without a database
# source. Nothing here is ever dialled.
BASE = ["--set", "database.url=postgres://u@example.invalid:5432/db"]
ENABLE_EVERYTHING = BASE + [
    "--set", "autoscaling.api.enabled=true",
    "--set", "gatewayApi.enabled=true",
    "--set", "ingress.enabled=true",
    "--set", "config.providerTimeoutDelay=1500ms",
    "--set", "config.shutdownDrainDelay=5s",
    "--set", "database.migrationsUrl=postgres://u@example.invalid:5432/direct",
]

# Every variable cmd/api reads, with a dedicated chart value.
API_ENV = (
    "HTTP_ADDR", "LOG_LEVEL", "DATABASE_URL", "MIGRATIONS_DATABASE_URL",
    "PROVIDER_TIMEOUT_DELAY", "SHUTDOWN_DRAIN_DELAY",
)


def helm_template(extra_args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["helm", "template", RELEASE, str(CHART_DIR), *extra_args],
        capture_output=True, text=True,
    )


def render(extra_args: list[str]) -> list[dict]:
    result = helm_template(extra_args)
    if result.returncode != 0:
        raise SystemExit(f"FAIL: helm template {' '.join(extra_args)}:\n{result.stderr}")
    try:
        import yaml  # type: ignore
    except ModuleNotFoundError:  # pragma: no cover - environment guard
        print("SKIP: PyYAML not available; cannot assert selectors", file=sys.stderr)
        raise SystemExit(0)
    return [d for d in yaml.safe_load_all(result.stdout) if d]


def selector_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("selector") or {}


def pod_labels_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("template", {}).get("metadata", {}).get("labels") or {}


def matches(selector: dict, labels: dict) -> bool:
    return bool(selector) and all(labels.get(k) == v for k, v in selector.items())


def env_of(dep: dict) -> list[dict]:
    return dep["spec"]["template"]["spec"]["containers"][0].get("env", [])


def env_names(dep: dict) -> list[str]:
    return [e["name"] for e in env_of(dep)]


def env_value(dep: dict, name: str) -> str | None:
    return next((e.get("value") for e in env_of(dep) if e["name"] == name), None)


def check_components(docs: list[dict], failures: list[str]) -> None:
    services = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Service"}
    deployments = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Deployment"}

    if set(deployments) != {RELEASE}:
        failures.append(f"the chart has exactly one Deployment (the api), rendered: {sorted(deployments)}")
    if set(services) != {RELEASE}:
        failures.append(f"the chart has exactly one Service (the api), rendered: {sorted(services)}")
    if RELEASE not in services:
        failures.append("the api Service was not rendered")
    elif selector_of(services[RELEASE]).get("app.kubernetes.io/component") != "api":
        failures.append("the api Service selector must pin component=api")

    # Every Deployment's own selector must pin a component too, and be
    # satisfied by its pod labels.
    for name, dep in deployments.items():
        match_labels = dep["spec"]["selector"].get("matchLabels") or {}
        if "app.kubernetes.io/component" not in match_labels:
            failures.append(f"Deployment {name} selector.matchLabels lacks app.kubernetes.io/component")
        if not matches(match_labels, pod_labels_of(dep)):
            failures.append(f"Deployment {name} pod labels do not satisfy its own selector")

    # The real invariant: each Service selects exactly one Deployment.
    for svc_name, svc in services.items():
        sel = selector_of(svc)
        hit = [d for d, dep in deployments.items() if matches(sel, pod_labels_of(dep))]
        if len(hit) != 1:
            failures.append(f"Service {svc_name} selects {len(hit)} Deployments {sorted(hit)}; expected exactly 1")

    # Ingress and HTTPRoute route to the api Service only; the HPA owns replicas.
    for d in docs:
        if d.get("kind") == "Ingress":
            backends = {p["backend"]["service"]["name"] for r in d["spec"]["rules"] for p in r["http"]["paths"]}
            if backends != {RELEASE}:
                failures.append(f"the Ingress must route to the api Service only, got {sorted(backends)}")
        if d.get("kind") == "HTTPRoute":
            backends = {b["name"] for r in d["spec"]["rules"] for b in r["backendRefs"]}
            if backends != {RELEASE}:
                failures.append(f"the HTTPRoute must route to the api Service only, got {sorted(backends)}")
    hpa = next((d for d in docs if d.get("kind") == "HorizontalPodAutoscaler"), None)
    if hpa is None or hpa["spec"]["scaleTargetRef"]["name"] != RELEASE:
        failures.append("the api HPA was not rendered (or does not scale the api Deployment)")
    elif "replicas" in deployments.get(RELEASE, {}).get("spec", {}):
        failures.append("the api Deployment must omit replicas when its HPA owns them")

    api = deployments.get(RELEASE)
    if api is None:
        failures.append("the api Deployment was not rendered")
        return
    api_env = env_names(api)
    for want in API_ENV:
        if want not in api_env:
            failures.append(f"the api Deployment does not render {want} from its dedicated value")
    duplicates = sorted({n for n in api_env if api_env.count(n) > 1})
    if duplicates:
        failures.append(f"the api Deployment renders env vars twice: {duplicates}")
    # No Kafka and no consumer: never rendered, under any values.
    for forbidden in api_env:
        if forbidden == "KAFKA_BROKERS" or forbidden.endswith("_CONSUMER_GROUP"):
            failures.append(f"the api Deployment renders {forbidden}; this service has no Kafka and no consumer")
    for name, want in (("PROVIDER_TIMEOUT_DELAY", "1500ms"), ("SHUTDOWN_DRAIN_DELAY", "5s")):
        if env_value(api, name) != want:
            failures.append(f"the api Deployment renders {name}={env_value(api, name)!r}, want {want!r}")


def check_refusals(failures: list[str]) -> None:
    cases: list[tuple[str, list[str], str]] = [
        ("without a database source", [], "requires database.url or database.existingSecret"),
        ("a *_MODE through extraEnv",
         BASE + ["--set", "extraEnv[0].name=CACHE_MODE", "--set", "extraEnv[0].value=permissive"], "extraEnv sets CACHE_MODE"),
        ("a consumer group through extraEnv",
         BASE + ["--set", "extraEnv[0].name=PAYMENTS_CONSUMER_GROUP", "--set", "extraEnv[0].value=pod"],
         "extraEnv sets PAYMENTS_CONSUMER_GROUP"),
        ("a chart-owned variable through extraEnv",
         BASE + ["--set", "extraEnv[0].name=SHUTDOWN_DRAIN_DELAY", "--set", "extraEnv[0].value=1s"], "extraEnv sets SHUTDOWN_DRAIN_DELAY"),
        ("a grace period under the shutdown bounds", BASE + ["--set", "terminationGracePeriodSeconds=20"], "terminationGracePeriodSeconds"),
        ("a grace period equal to the shutdown bounds", BASE + ["--set", "terminationGracePeriodSeconds=25"], "must exceed 25"),
        ("an http address on another port than the container's", BASE + ["--set", "config.httpAddr=:9090"], "config.httpAddr"),
    ]
    for label, args, needle in cases:
        result = helm_template(args)
        if result.returncode == 0 or needle not in result.stderr:
            failures.append(f"chart rendered (or failed for another reason) {label}")


def main() -> int:
    failures: list[str] = []

    check_components(render(ENABLE_EVERYTHING), failures)

    # Default values must not deploy an HPA or a route, nor any optional env.
    defaults = render(BASE)
    stray = [d["kind"] for d in defaults if d.get("kind") in {"HorizontalPodAutoscaler", "Ingress", "HTTPRoute"}]
    if stray:
        failures.append(f"optional templates rendered with default values: {stray}")
    dep = next(d for d in defaults if d.get("kind") == "Deployment")
    names = env_names(dep)
    off = {"PROVIDER_TIMEOUT_DELAY", "SHUTDOWN_DRAIN_DELAY"}
    if off & set(names):
        failures.append(f"these must be off with default values: {sorted(off & set(names))}")
    for want in ("HTTP_ADDR", "LOG_LEVEL", "DATABASE_URL", "MIGRATIONS_DATABASE_URL"):
        if want not in names:
            failures.append(f"{want} is mandatory and must always be rendered")

    check_refusals(failures)

    if failures:
        for f in failures:
            print(f"FAIL: {f}")
        return 1

    print("PASS: the Service selects exactly one Deployment (the api); the HPA, Ingress and HTTPRoute are off by default "
          "and route to the api only; every cmd/api env var has a dedicated value; no Kafka or consumer-group variable is "
          "ever rendered; the chart refuses to render without a database source, with a *_MODE or a consumer group in "
          "extraEnv, with a grace period that does not exceed the shutdown bounds, or with an http address off the "
          "container port")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
