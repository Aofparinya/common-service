# Order Platform Common Service

Go service for master data, non-secret configuration, feature flags and central document numbers.

- Port: `3006`
- PostgreSQL schema: `common`
- Thai address tables: 77 provinces, 928 districts and 7,452 subdistricts

## Thai address APIs

```text
GET /api/v1/locations/provinces
GET /api/v1/locations/districts?provinceCode=10
GET /api/v1/locations/subdistricts?districtCode=1001
GET /api/v1/locations/search?q=สามเสนนอก
GET /api/v1/locations/search?postalCode=10310
```

The database is seeded idempotently from
[`github.com/ultramcu/go-thaiaddress`](https://github.com/ultramcu/go-thaiaddress)
version `v0.2.0`, an MIT-licensed embedded snapshot based on Thai DOPA geocodes.

```powershell
go mod tidy
go test ./...
go run ./cmd/server
```
