# Changelog

## 2026-11-09

- **fix:** Honour `schemas` and `exclude` in `env` blocks when inspecting and diffing the dev database and HCL schemas. ([pending](pending))
- **fix:** Fail on destructive statements such as `DROP SCHEMA ... CASCADE` unless `--allow-destructive` is set. ([pending](pending))
- **chore:** Update Go dependencies, including Testcontainers v0.45.0 and go-cty v1.20.0; Atlas remains at v1.3.0, the latest release. ([pending](pending))

## 2026-10-12

- **fix:** Avoid replaying migrations into databases with extensions or foreign servers, and check round-trip diffs by migration name. ([34426a5](https://github.com/edlundin/edvat/commit/34426a54d354cf5c10e0cc53d662b819ed664659))
- **chore:** Update Go dependencies and GitHub Actions; Atlas remains at v1.3.0. ([305fab4](https://github.com/edlundin/edvat/commit/305fab4743e312a4852be976ea8af8ced1d979e8))

## 2026-09-01

- **test:** Align migration replay assertions with normalized SQL output. ([b3aa797](https://github.com/edlundin/edvat/commit/b3aa7976e127b7023958b4b98398769a3e4ec583))
- **chore:** Update Go, Atlas, Testcontainers, and related dependencies. ([8af0a77](https://github.com/edlundin/edvat/commit/8af0a77bbb4c7ad1d5320390691790b97c4a9c9d))
