# HTTP Contracts

`typespec/main.tsp` describes the current HTTP surface implemented by `apps/api`.

It includes:

- voting CRUD and lifecycle fields
- per-voting anti-abuse configuration
- vote challenge issuance
- vote submission with PoW and interaction metadata
- vote delivery status lookup
- results and policy endpoints

Keep this contract aligned with the request and response structs in `apps/api/internal/domain/domain.go` and the handlers in `apps/api/internal/app/app.go`.
