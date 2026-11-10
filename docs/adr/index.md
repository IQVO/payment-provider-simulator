---
id: index
title: Architecture decision records
sidebar_label: ADR index
slug: /
sidebar_position: 0
---

# Architecture decision records

The decisions of `payment-provider-simulator`, in the order they were taken. The
ADR bodies are immutable; a changed decision gets a new ADR that supersedes the
old one. The *Status* column is copied from each ADR's own status line.

| ADR | Title | Status |
| --- | --- | --- |
| [0001](/docs/adr/0001-payment-provider-simulator-bounded-context) | Payment Provider Simulator is a bounded context of the storefront domain | Accepted (bootstrap). Design source: warehouse-docs `storefront/model/model.yaml`, context `payment-provider-simulator`. |
| [0002](/docs/adr/0002-payment-provider-simulator-assumptions) | Payment Provider Simulator assumptions | Accepted. Contract source: customer-payments `docs/adr/0002-customer-payments-assumptions.md`, assumption A12. |
| [0003](/docs/adr/0003-fleet-observability-standard) | Adopt the fleet observability standard (logs, metrics, traces) | Accepted (2026-10-29). |
| [0004](/docs/adr/0004-adopt-the-fleet-rest-api-guideline) | Adopt the fleet REST API guideline | Accepted. Date: 2026-11-01. |
