# Event Contracts

This directory holds machine-readable contracts for Kafka events.

- `asyncapi/` describes channels, producers, consumers, and message bindings.
- `schemas/jsonschema/` defines payload formats for each topic.
- `../topics/topics.yaml` is the source of truth for topic provisioning.

Keep these contracts aligned with the domain structs used by `apps/api` and `apps/projector`, especially for:

- `votes.raw`
- `votings.catalog`
- `voting-catalog-latest`
- `voting-results-snapshot`
