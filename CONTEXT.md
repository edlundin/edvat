# Context

## Glossary

### Migration Planning

The process of turning a desired database definition and optional current database state into migration statements that are safe to review and apply.

### Migration Plan

The ordered migration statements and review findings produced by Migration Planning.

### Object Family

A category of PostgreSQL object managed consistently as a set, such as extensions, domains, sequences, functions, policies, permissions, or views.

### Current Database State

The database objects and seed rows observed from a development database before building a Migration Plan.

### Desired Database Definition

The database objects and seed data read from configured schema and seed sources before building a Migration Plan.

### Statement Dependency

A required ordering relationship between migration statements, where one statement must run before another because the later statement refers to something the earlier statement creates or changes.

### Seed Data Planning

The process of turning desired seed data and an optional current database state into data-changing migration statements.
