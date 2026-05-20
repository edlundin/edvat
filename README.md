# Edvat

Edvat is a small PostgreSQL migration authoring tool built on top of Atlas OSS. Atlas handles the ordinary schema work; Edvat adds support for PostgreSQL object families that Atlas OSS does not fully manage in this workflow, then writes reviewable SQL migrations.

## What it does

- Generates `*.up.sql` and `*.down.sql` migrations from HCL schema files.
- Rewrites `atlas.sum` for a migration directory.
- Delegates core schemas, tables, columns, indexes, constraints, and enums to Atlas OSS.
- Adds experimental PostgreSQL support for extensions, sequences, partitions, exclusion constraints, domains, composites, ranges, collations, casts, views, materialized views, functions, procedures, aggregates, triggers, event triggers, policies, permissions, default privileges, foreign servers, foreign tables, user mappings, roles, users, and seed data.
- Blocks destructive SQL unless explicitly allowed.

## Install

```sh
go install github.com/edlundin/edvat/cmd/edvat@latest
```

## Atlas-compatible project files

Edvat reads the same `atlas.hcl` shape you use with Atlas: an `env`, one or more schema sources, and a migration directory.

Minimal project:

```hcl
env "local" {
  schema { src = "schema.pg.hcl" }
  migration { dir = "migrations" }
}
```

Import a schema directory:

```hcl
env "local" {
  schema { src = "schemas" } # all *.hcl files in schemas/
  migration { dir = "migrations" }
}
```

Import multiple schema files:

```hcl
env "local" {
  schema { src = ["schemas/common.pg.hcl", "schemas/users.pg.hcl"] }
  migration { dir = "migrations" }
}
```

Use Atlas-style `data.hcl_schema` imports:

```hcl
data "hcl_schema" "app" {
  paths = ["schemas/common.pg.hcl", "schemas/users.pg.hcl"]
}

env "local" {
  schema { src = data.hcl_schema.app.url }
  migration { dir = "migrations" }
}
```

Seed data is optional:

```hcl
env "local" {
  schema { src = "schemas" }
  migration { dir = "migrations" }

  data {
    mode = UPSERT # INSERT, UPSERT, or SYNC
    src  = ["seed/countries.sql"]
  }
}
```

## CLI usage

Show supported object families:

```sh
edvat capabilities
```

Hash the migration directory:

```sh
edvat migrate hash --env local --config atlas.hcl
```

Create a migration from the desired schema only:

```sh
edvat migrate diff add_users --env local --config atlas.hcl
```

Create a migration using a dev database as current state:

```sh
edvat migrate diff add_users \
  --env local \
  --config atlas.hcl \
  --dev-url "postgres://postgres:pass@localhost:5432/app?sslmode=disable&search_path=public"
```

Destructive statements fail by default. Allow them only after review:

```sh
edvat migrate diff drop_old_column --env local --config atlas.hcl --allow-destructive
```

Role and user DDL are opt-in:

```sh
edvat migrate diff security_change --manage-roles --manage-users --env local --config atlas.hcl
```

## Example schema

```hcl
schema "public" {}

extension "pgcrypto" {
  schema = schema.public
}

permission "app_read_users" {
  on         = table.users
  to         = "app_reader"
  privileges = [SELECT]
}

default_permission "reader_execute_functions" {
  schema     = schema.public
  for_role   = "app_owner"
  on         = FUNCTIONS
  to         = "app_reader"
  privileges = [EXECUTE]
}
```

## Development

```sh
go test ./...
go run ./cmd/edvat capabilities
```

Some integration tests use Docker/Testcontainers with PostgreSQL.

## License

Apache-2.0. See [LICENSE](LICENSE).
