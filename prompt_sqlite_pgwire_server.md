# PostgreSQL Wire Protocol Server with SQLite and Blob Storage - Complete Specification

## 1. OVERVIEW

Build a lightweight PostgreSQL-compatible server that:
- Implements PostgreSQL Wire Protocol v3 using the `psql-wire` library (Go)
- Uses SQLite as the data backend
- Stores SQLite data in blob storage (S3, Azure Blob Storage, or local filesystem)
- Provides full ACID support and locking with SQLite transactions
- Supports BEGIN/COMMIT/ROLLBACK
- Compatible with standard PostgreSQL clients (psql, JDBC, Python psycopg2, etc.)
- Minimal footprint - ready for deployment on edge/edge computing environments

## 2. TECHNICAL REQUIREMENTS

### Technology Stack:
- **Language:** Go (using psql-wire library: github.com/jeroenrinzema/psql-wire)
- **Database:** SQLite with persistence
- **Blob Storage:** Support for:
  - Amazon S3
  - Azure Blob Storage
  - Local filesystem (for development)
- **Port:** Default 5432 (standard PostgreSQL port)

### Required Libraries:
- `github.com/jeroenrinzema/psql-wire` - PostgreSQL wire protocol
- `github.com/mattn/go-sqlite3` - SQLite Driver
- `github.com/aws/aws-sdk-go-v2` - AWS S3 (optional)
- `github.com/Azure/azure-sdk-for-go` - Azure Blob Storage (optional)

## 3. SYSTEM ARCHITECTURE

```
┌─────────────────────────────────────────────────────────┐
│         PostgreSQL Clients (psql, JDBC, etc.)          │
└────────────────┬────────────────────────────────────────┘
                 │ PostgreSQL Wire Protocol v3
┌────────────────▼────────────────────────────────────────┐
│     PostgreSQL Wire Protocol Server (psql-wire)         │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Authentication & Connection Manager              │   │
│  │ - Startup message handling                       │   │
│  │ - Password authentication                        │   │
│  │ - Session state management                       │   │
│  └──────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Query Processor                                  │   │
│  │ - Simple Query Protocol                          │   │
│  │ - Extended Query Protocol (prepared statements) │   │
│  │ - Transaction state tracking (BEGIN/COMMIT)     │   │
│  └──────────────────────────────────────────────────┘   │
└────────────────┬────────────────────────────────────────┘
                 │
┌────────────────▼────────────────────────────────────────┐
│           SQLite Database Engine                        │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Transaction Manager                              │   │
│  │ - ACID compliance                                │   │
│  │ - Locking (SHARED/RESERVED/EXCLUSIVE)           │   │
│  │ - BEGIN/COMMIT/ROLLBACK                         │   │
│  └──────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────┐   │
│  │ Query Execution Engine                           │   │
│  │ - SQL parsing                                    │   │
│  │ - Query optimization                            │   │
│  │ - Result streaming                              │   │
│  └──────────────────────────────────────────────────┘   │
└────────────────┬────────────────────────────────────────┘
                 │
┌────────────────▼────────────────────────────────────────┐
│       Blob Storage Manager                              │
│  ┌──────────────────────────────────────────────────┐   │
│  │ SQLite Database File Persistence Layer           │   │
│  │ - Download from blob storage (S3/Azure/Local)   │   │
│  │ - Upload after transactions                      │   │
│  │ - Versioning & snapshots                         │   │
│  └──────────────────────────────────────────────────┘   │
└────────────────┬────────────────────────────────────────┘
                 │
        ┌────────┴────────┬──────────────┬──────────────┐
        │                 │              │              │
    ┌───▼──┐         ┌───▼──┐      ┌───▼──┐      ┌───▼──┐
    │ S3   │         │Azure │      │Local │      │Cache │
    │      │         │Blob  │      │FS    │      │      │
    └──────┘         └──────┘      └──────┘      └──────┘
```

## 4. DETAILED FUNCTIONAL REQUIREMENTS

### 4.1 PostgreSQL Wire Protocol Implementation

**Startup Phase:**
- Handle `StartupMessage` with parameters: user, database, application_name
- Handle authentication (plain password, md5, SCRAM-SHA-256)
- Send `ParameterStatus` containing:
  - `server_version` → "13.0" (emulate PostgreSQL 13)
  - `server_encoding` → "UTF8"
  - `client_encoding` → "UTF8"
  - `DateStyle` → "ISO, MDY"
  - `application_name` → value from request
- Send `ReadyForQuery` at end of handshake

**Query Processing:**
- **Simple Query Protocol:**
  - Accept SQL query as string
  - Parse multiple statements separated by `;`
  - For each statement send `RowDescription`, `DataRow`, `CommandComplete`
  - Handle error messages with PostgreSQL error codes

- **Extended Query Protocol:**
  - Handle `Parse` (prepare statement)
  - Handle `Bind` (assign parameters)
  - Handle `Execute` (execute)
  - Handle `Describe` (return column metadata)
  - Cache prepared statements in memory per-connection

- **Transaction Support:**
  - BEGIN - open transaction
  - COMMIT - finalize changes
  - ROLLBACK - undo changes
  - Track transaction status: idle, in-transaction, failed-transaction
  - Send `ReadyForQuery` with status (I=idle, T=transaction, E=error)

- **COPY Protocol:**
  - Support `COPY TO` for data export
  - Support `COPY FROM` for data import
  - Return data in CSV or text format

### 4.2 SQLite Backend

**Database Connection:**
- Open/load SQLite database from blob storage
- Maintain connection pool (max 10 connections)
- Per-connection transaction tracking

**Transaction Management:**
- BEGIN DEFERRED (default) - start without lock
- BEGIN IMMEDIATE - acquire lock immediately
- BEGIN EXCLUSIVE - enforce exclusive lock
- SAVEPOINT - support nested transactions
- Implement proper locking sequence (SHARED → RESERVED → EXCLUSIVE)

**Query Execution:**
- Parse SQL queries
- Execute prepare statement (for security)
- Return results in PostgreSQL format (binary + text)
- Support all standard SQLite functions (strftime, substr, etc.)
- Map SQLite types → PostgreSQL types:
  - NULL → NULL
  - INTEGER → int8
  - REAL → float8
  - TEXT → text
  - BLOB → bytea

**Error Handling:**
- Map SQLite error codes → PostgreSQL error codes
  - SQLITE_CONSTRAINT → 23000 (integrity constraint violation)
  - SQLITE_IOERR → 08006 (connection failure)
  - etc.

### 4.3 Blob Storage Integration

**Storage Backends:**

#### Local Filesystem (Development):
- Store `.sqlite` files in `./data/` directory
- Automatically create directory if it doesn't exist
- Format: `./data/{database_name}.sqlite`
- Handle .wal (Write-Ahead Log) files if present

#### S3 Integration:
- Environment variables:
  - `STORAGE=s3`
  - `S3_BUCKET=my-databases`
  - `S3_REGION=us-east-1`
  - `S3_PREFIX=sqlite-dbs/`
- Handle AWS credentials (IAM role or access keys)
- Download database at startup: `s3://{bucket}/{prefix}{database_name}.sqlite`
- Upload after each commit (or every N minutes - async)
- Support versioning (keep old versions with timestamp)

#### Azure Blob Storage:
- Environment variables:
  - `STORAGE=azure`
  - `AZURE_STORAGE_ACCOUNT=myaccount`
  - `AZURE_STORAGE_CONTAINER=sqlite-dbs`
  - `AZURE_STORAGE_KEY=...` or `AZURE_IDENTITY_CLIENT_ID=...`
- Download: `https://{account}.blob.core.windows.net/{container}/{database_name}.sqlite`
- Upload after each commit
- Support versioning (blob snapshots)

**Caching Strategy:**
- Download database from blob storage on startup
- Store in `/tmp/{database_name}-{unique_id}.sqlite`
- On graceful shutdown: upload to blob storage
- Implement watchdog - upload every 5 minutes if changes detected

**Concurrency Control (Multi-Instance):**
- If multiple server instances read from same database:
  - Implement optimistic concurrency control
  - On COMMIT: check if blob version changed since download
  - If yes: ROLLBACK with error `40001` (serialization failure)
  - Client should retry transaction

## 5. CONFIGURATION

### Environment Variables:
```
# PostgreSQL Wire Server
PG_PORT=5432
PG_HOST=0.0.0.0
PG_USER=postgres
PG_PASSWORD=mysecretpassword

# Database
DB_NAME=myapp
DB_PATH=/tmp/myapp.sqlite

# Storage Backend
STORAGE=local|s3|azure           # Default: local
S3_BUCKET=                        # Only for S3
S3_REGION=us-east-1             # Only for S3
S3_PREFIX=sqlite-dbs/            # Only for S3
AZURE_STORAGE_ACCOUNT=           # Only for Azure
AZURE_STORAGE_CONTAINER=         # Only for Azure
AZURE_STORAGE_KEY=               # Only for Azure (alternative: use managed identity)

# Logging
LOG_LEVEL=debug|info|warn|error  # Default: info
LOG_FORMAT=json|text             # Default: text

# Performance
CONNECTION_POOL_SIZE=10
CACHE_TTL_MINUTES=5              # How often to sync with blob storage
```

### Config File (config.yaml):
```yaml
server:
  port: 5432
  host: 0.0.0.0
  authentication:
    user: postgres
    password: mysecretpassword

database:
  name: myapp
  sqlite_path: /tmp/myapp.sqlite
  transaction_mode: deferred  # deferred, immediate, exclusive

storage:
  backend: local  # local, s3, azure
  local:
    base_path: ./data
  s3:
    bucket: my-databases
    region: us-east-1
    prefix: sqlite-dbs/
  azure:
    account: myaccount
    container: sqlite-dbs
    use_managed_identity: true

logging:
  level: info
  format: json
```

## 6. API/INTERFACE

### Go Server Interface:
```go
type PGSQLiteServer struct {
    config    *Config
    db        *sql.DB
    storage   BlobStorage
    listeners map[string]*ClientConnection
}

func (s *PGSQLiteServer) Start() error
func (s *PGSQLiteServer) Stop() error
func (s *PGSQLiteServer) HandleConnection(conn net.Conn)
func (s *PGSQLiteServer) ExecuteQuery(ctx context.Context, query string, params []interface{}) (*QueryResult, error)
func (s *PGSQLiteServer) BeginTransaction(connectionID string) error
func (s *PGSQLiteServer) CommitTransaction(connectionID string) error
func (s *PGSQLiteServer) RollbackTransaction(connectionID string) error

type BlobStorage interface {
    Download(ctx context.Context, dbName string) (io.Reader, error)
    Upload(ctx context.Context, dbName string, data io.Reader) error
    List(ctx context.Context) ([]string, error)
    Delete(ctx context.Context, dbName string) error
}
```

## 7. TESTING STRATEGY

### Unit Tests:
- Wire protocol parsing (startup, query, bind, execute)
- SQLite connection pool
- Transaction state machine
- Blob storage backends (mock S3/Azure)

### Integration Tests:
- Full query flow: client → wire protocol → SQLite → results
- Transaction ACID properties
- Multi-connection concurrency
- Error handling

### E2E Tests (with psql):
```bash
# Test basic query
psql -h localhost -p 5432 -U postgres -d myapp -c "SELECT 1"

# Test transaction
psql -h localhost -p 5432 -U postgres -d myapp << EOF
BEGIN;
CREATE TABLE test (id INT, name TEXT);
INSERT INTO test VALUES (1, 'Alice');
INSERT INTO test VALUES (2, 'Bob');
COMMIT;
SELECT * FROM test;
EOF

# Test concurrent connections
for i in {1..5}; do
  psql -h localhost -p 5432 -U postgres -d myapp -c "SELECT $i" &
done
wait
```

## 8. MONITORING & OBSERVABILITY

### Metrics to expose:
- `pg_connections_active` - number of active connections
- `pg_queries_total` - total queries executed
- `pg_queries_duration_seconds` - query execution time histogram
- `pg_transactions_total` - total transactions (by status: commit/rollback)
- `pg_blob_storage_operations_total` - upload/download operations
- `sqlite_locks_held` - current SQLite locks (SHARED/RESERVED/EXCLUSIVE)

### Logs:
- Connection events (connect, disconnect, auth fail)
- Query execution (slow queries > 100ms)
- Transaction events (BEGIN, COMMIT, ROLLBACK, DEADLOCK)
- Storage events (download, upload, sync errors)
- Errors with full context (query, client, transaction)

## 9. DEPLOYMENT CONSIDERATIONS

### Docker:
```dockerfile
FROM golang:1.21-alpine
WORKDIR /app
COPY . .
RUN go build -o pgserver main.go
EXPOSE 5432
CMD ["./pgserver"]
```

### Kubernetes:
- Expose port 5432 via Service
- Liveness probe: `SELECT 1`
- Readiness probe: verify blob storage connection
- Volume mounts: `/tmp` for SQLite files (ephemeral storage)

### Edge/Lightweight Deployment:
- Binary size: < 50MB (Go static binary)
- Memory usage: < 100MB per server instance
- CPU: minimal (event-driven)
- Disk: dynamic (grows with database size)

## 10. EXAMPLE CLIENT INTERACTIONS

### Python (psycopg2):
```python
import psycopg2

conn = psycopg2.connect(
    host="localhost",
    port=5432,
    user="postgres",
    password="mysecretpassword",
    database="myapp"
)
cursor = conn.cursor()

# Simple query
cursor.execute("SELECT * FROM users")
print(cursor.fetchall())

# Transaction
try:
    cursor.execute("BEGIN")
    cursor.execute("INSERT INTO users (name) VALUES (%s)", ("Alice",))
    cursor.execute("COMMIT")
except Exception as e:
    cursor.execute("ROLLBACK")
    raise

conn.close()
```

### JavaScript (node-postgres):
```javascript
const { Client } = require('pg');

const client = new Client({
  host: 'localhost',
  port: 5432,
  user: 'postgres',
  password: 'mysecretpassword',
  database: 'myapp',
});

await client.connect();

// Transaction
const res = await client.query('BEGIN');
try {
  await client.query('INSERT INTO users (name) VALUES ($1)', ['Alice']);
  await client.query('COMMIT');
} catch (e) {
  await client.query('ROLLBACK');
  throw e;
}

await client.end();
```

### JDBC (Java):
```java
import java.sql.*;

String url = "jdbc:postgresql://localhost:5432/myapp";
String user = "postgres";
String password = "mysecretpassword";

Connection conn = DriverManager.getConnection(url, user, password);

// Transaction
try {
    conn.setAutoCommit(false);
    Statement stmt = conn.createStatement();
    stmt.executeUpdate("INSERT INTO users (name) VALUES ('Alice')");
    conn.commit();
} catch (SQLException e) {
    conn.rollback();
    throw e;
}

conn.close();
```

## 11. EDGE CASES & ERROR HANDLING

1. **Network Disconnection during Transaction:**
   - Server should rollback automatically after timeout (30s)
   - Log the event

2. **Blob Storage Unavailable:**
   - On startup: fail fast with clear error message
   - During runtime: maintain in-memory cache, retry async
   - Alert operator

3. **SQLite Database Locked (concurrent writers):**
   - Return `40001` (serialization failure) to client
   - Client should retry with backoff

4. **Out of Disk Space:**
   - Catch SQLite error, return `53100` (disk full)
   - Stop accepting new transactions

5. **Large Result Sets:**
   - Stream results row by row (don't load everything to memory)
   - Implement LIMIT/OFFSET for pagination

## 12. FUTURE ENHANCEMENTS

- [ ] Replication (sync to multiple blob stores)
- [ ] Connection pooling improvements (PgBouncer-like)
- [ ] Query caching (memcached integration)
- [ ] Time-travel queries (point-in-time recovery)
- [ ] Full PostgreSQL compatibility (views, triggers, functions)
- [ ] GraphQL API wrapper
- [ ] Row-level security (RLS)

## 13. DELIVERABLES

1. **Main server binary** - `pgserver` (Go)
2. **Docker image** - `sqlite-pgwire:latest`
3. **Configuration files** - `config.yaml.example`, `.env.example`
4. **Documentation** - README with examples
5. **Tests** - unit + integration
6. **Monitoring** - Prometheus metrics, example Grafana dashboard
7. **Examples** - Python, JavaScript, Java clients

