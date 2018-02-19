# generic-rw-aurora

[![Circle CI](https://circleci.com/gh/Financial-Times/generic-rw-aurora/tree/master.png?style=shield)](https://circleci.com/gh/Financial-Times/generic-rw-aurora/tree/master)[![Go Report Card](https://goreportcard.com/badge/github.com/Financial-Times/generic-rw-aurora)](https://goreportcard.com/report/github.com/Financial-Times/generic-rw-aurora) [![Coverage Status](https://coveralls.io/repos/github/Financial-Times/generic-rw-aurora/badge.svg)](https://coveralls.io/github/Financial-Times/generic-rw-aurora)

Generic r/w app for Aurora

## How to build

```
go get -u github.com/kardianos/govendor
cd $GOPATH/src/github.com/Financial-Times
git clone git@github.com:Financial-Times/generic-rw-aurora.git
cd $GOPATH/src/github.com/Financial-Times/generic-rw-aurora
govendor sync
go build
```


## How to test

Run with `-short` to skip database integration tests.

To run database integration tests, you must set the environment variable `DB_TEST_URL` to a connection string for a MySQL database, with credentials that have privileges to create databases and users. The test cases will provision a test user `pac_test_user` and up-to-date schema in the database.


To test locally run a local instance of mySql. Use the version specified in circleci config.
e.g. export DB_TEST_URL=username:password@tcp(127.0.0.1:3306)/dbname

## Endpoints

For each `path` listed in the configuration file (see below), the service creates `GET` and `PUT` endpoints.

The application also has the standard `/__health`, `/__gtg` and `/__build-info` endpoints.

## Configuration

Table schemas can be managed by Goose. The versions are stored in `db/schema.go`.
In practice, rollback steps are listed for reference only; they must be applied manually if required.

Note that _every_ table used by this service requires a `hash` column, even if write conflict detection (see below) is not enabled.

The application requires a YAML configuration file to map between HTTP endpoints and tables in the Aurora database.

The root object for the configuration is `paths`, which contains a mapping between URL paths and persistence stores. Paths may contain `:param-name` placeholders, which are recognised in the routing library.

A path is mapped to a table, a mapping of columns to expressions, and an optional mapping of columns to response headers. The primary key column must also be specified.

### Write mapping
The expressions for column values may contain the following syntax:
- `:name` extracts a value from the incoming request (a path or query string parameter)
- `@.name` extracts a value from the metadata for the incoming request. The name `_timestamp` is populated by the request time and all HTTP headers are propagated into the metadata (with header names forced into lower case).
- `$` extracts the entire request body
- `$.name` extracts a JSON path from the request body

### Read mapping
The response body is the column whose value is the document itself (`$`).
- For JSON documents, additional columns may be embedded on read by mapping them in the `response.body` section.
- If write conflict detection is enabled, then the `Document-Hash` header is automatically included in the response. Other headers may be extracted from columns by specifying them in the `response.headers` section. Quoting the names will preserve the case of the header name.

For example:
```
paths:
  "/drafts/content/:id/annotations":
    table: draft_annotations
    columns:
      uuid: ":id"
      last_modified: "@.timestamp"
      publish_ref: "@.publishRef"
      origin_system: "@.x-origin-system-id"
      body: "$"
    primaryKey: uuid
    hasConflictDetection: true
    response:
      body:
        lastModified: last_modified
      headers:
        "X-Origin-System-Id": origin_system
  "/published/content/:id/annotations":
    ...
```

## Write conflict detection 

It is possible to enable write conflict detection on a specific endpoint by 
setting to `true` the `hasConflictDetection` property in the YAML configuration file.

To use this feature, clients MUST set the special HTTP header `Previous-Document-Hash` 
when they update an existing document. 
The value of such header MUST be the hash of the current document body stored 
in the database that the client wants to update. 
The hash of the document is returned by each GET and PUT response in the `Document-Hash` 
HTTP header.
