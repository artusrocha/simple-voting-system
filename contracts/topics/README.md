# Topic Contracts

`topics.yaml` is the source of truth for Kafka topic names and broker-level topic settings used by the local stack.

- Keep topic names aligned with environment variables consumed by `apps/api` and `apps/projector`.
- Keep payload schemas in `../events/schemas/`.
- Keep higher-level event flow descriptions in `../events/asyncapi/`.

The CI workflow validates that AsyncAPI channel addresses remain aligned with `topics.yaml`.
