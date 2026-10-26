# 1. Payment Provider Simulator is a bounded context of the storefront domain

Status: Accepted (bootstrap). Design source: warehouse-docs `storefront/model/model.yaml`, context `payment-provider-simulator`.

## Context

The storefront is the e-commerce domain of the retail-network organisation: six bounded contexts, each its own
service and database. Go stand-in for the external payment provider (own Postgres): tokenize (hosted-fields stand-in), authorize, capture, void, refund, re-authorize and reduce (partial release) with deterministic outcomes per test token (approve, decline, timeout, expire-early). The HTTP contract is the one customer-payments documents in ADR 0002 A12; swapping it for a real PSP changes nothing in the storefront.

## Decision

- This repo owns the aggregates ProviderAuthorization; the invariants and error names are those of the model.
- Business decisions of the context:
- Deterministic outcomes keyed on the test token, so the storefront's saga tests are reproducible.
- Every POST is idempotent on the Idempotency-Key header; the same key returns the first answer.
- Never sees a card number, only tokens it issued itself.
- Events use the storefront CloudEvents namespace `com.retailnetwork.storefront.payment-provider-simulator.<entity>.<Event>` on `retailnetwork.payment-provider-simulator.events`
  (analytics-only events on `.analytics`). Published: none (read side). Consumed: none. No `warehouse.*` topic is consumed or produced.
- The storefront calls only the network's Open Host Service routes (model inbound rows for `shopper / storefront`),
  never an in-org or restricted route; sibling data used in a decision is a local event-fed read model, except the
  calls the model lists under `sync_calls`.
- No authentication on REST, consistent with the fleet and the network. This context stores no PII and no card data.

## Consequences

Relationships and the full consumed-event list are in the model and in the AsyncAPI contract added with the first feature PR.
