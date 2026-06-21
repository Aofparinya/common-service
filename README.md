# Order Platform Common Service

Go service for master data, non-secret configuration, feature flags and central document numbers.

- Port: `3006`
- PostgreSQL schema: `common`

```powershell
go mod tidy
go test ./...
go run ./cmd/server
```
