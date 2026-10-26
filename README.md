# payment-provider-simulator

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Payment Provider Simulator** (Generic, storefront tier).

Go stand-in for the external payment provider (own Postgres): tokenize (hosted-fields stand-in), authorize, capture, void, refund, re-authorize and reduce (partial release) with deterministic outcomes per test token (approve, decline, timeout, expire-early). The HTTP contract is the one customer-payments documents in ADR 0002 A12; swapping it for a real PSP changes nothing in the storefront.

Status: under construction. The design lives in [warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/) `storefront/model/model.yaml` (single source of truth).
This repository is part of the storefront domain: it consumes the retail network's Open Host Service routes and its
published `retailnetwork.*` events; it never calls an in-org or restricted network route.

Runtime: Go, hexagonal, real PostgreSQL and Kafka only (`DATABASE_URL`, `KAFKA_BROKERS` are mandatory), CloudEvents 1.0 on every message
(`com.retailnetwork.storefront.payment-provider-simulator.<entity>.<Event>`), transactional outbox.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

See `docs/adr/` for decisions and `docs/before-first-release.md` for the packaging checklist.
