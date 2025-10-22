# PostgreSQL Wire Protocol Server with SQLite Backend

A lightweight PostgreSQL-compatible server that uses SQLite as the backend and stores data in blob storage (S3, Azure, or local filesystem). This allows you to use PostgreSQL client tools and libraries with SQLite databases stored in cloud blob storage.

## Features

- **PostgreSQL Wire Protocol v3**: Full compatibility with PostgreSQL clients (psql, JDBC, Python psycopg2, Node.js pg, etc.)
- **SQLite Backend**: Lightweight, serverless database engine with ACID compliance
- **Blob Storage Support**: Store SQLite databases in:
  - Amazon S3
  - Azure Blob Storage
  - Local filesystem (for development)
- **Transaction Support**: Full BEGIN/COMMIT/ROLLBACK support with proper locking
- **Connection Pooling**: Efficient connection management
- **Auto-sync**: Automatic synchronization with blob storage after commits
- **Lightweight**: Minimal footprint suitable for edge deployments

## Architecture

```
PostgreSQL Client → Wire Protocol → SQLite Engine → Blob Storage
(psql, JDBC, etc.)   (Port 5432)    (In-memory)     (S3/Azure/Local)
```

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/harcerz/pgblob.git
cd pgblob

# Build the server
go build -o pgserver

# Or install directly
go install
```

### Configuration

Create a `config.yaml` file or use environment variables:

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

storage:
  backend: local
  local:
    base_path: ./data
```

Or use environment variables:

```bash
export PG_PORT=5432
export PG_USER=postgres
export PG_PASSWORD=mysecretpassword
export DB_NAME=myapp
export STORAGE=local
```

### Run the Server

```bash
# Using config file
./pgserver

# Using environment variables
PG_PASSWORD=secret ./pgserver

# Using custom config path
CONFIG_PATH=/etc/pgserver/config.yaml ./pgserver
```

### Connect with psql

```bash
psql -h localhost -p 5432 -U postgres -d myapp
```

## Usage Examples

### Python (psycopg2)

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

# Create table
cursor.execute("""
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    )
""")

# Insert data
cursor.execute("INSERT INTO users (id, name, email) VALUES (%s, %s, %s)",
               (1, "Alice", "alice@example.com"))
conn.commit()

# Query data
cursor.execute("SELECT * FROM users")
for row in cursor.fetchall():
    print(row)

conn.close()
```

### JavaScript (node-postgres)

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

// Create table
await client.query(`
  CREATE TABLE IF NOT EXISTS products (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    price REAL
  )
`);

// Insert with transaction
await client.query('BEGIN');
try {
  await client.query('INSERT INTO products (id, name, price) VALUES ($1, $2, $3)',
                    [1, 'Widget', 9.99]);
  await client.query('COMMIT');
} catch (e) {
  await client.query('ROLLBACK');
  throw e;
}

// Query
const res = await client.query('SELECT * FROM products');
console.log(res.rows);

await client.end();
```

### Java (JDBC)

```java
import java.sql.*;

String url = "jdbc:postgresql://localhost:5432/myapp";
Properties props = new Properties();
props.setProperty("user", "postgres");
props.setProperty("password", "mysecretpassword");

Connection conn = DriverManager.getConnection(url, props);

// Create table
Statement stmt = conn.createStatement();
stmt.executeUpdate("CREATE TABLE IF NOT EXISTS items (id INT PRIMARY KEY, name TEXT)");

// Transaction
conn.setAutoCommit(false);
try {
    PreparedStatement pstmt = conn.prepareStatement("INSERT INTO items VALUES (?, ?)");
    pstmt.setInt(1, 1);
    pstmt.setString(2, "Item 1");
    pstmt.executeUpdate();
    conn.commit();
} catch (SQLException e) {
    conn.rollback();
    throw e;
}

conn.close();
```

## Storage Backends

### Local Filesystem

Best for development and testing.

```yaml
storage:
  backend: local
  local:
    base_path: ./data
```

### Amazon S3

Store databases in AWS S3:

```yaml
storage:
  backend: s3
  s3:
    bucket: my-databases
    region: us-east-1
    prefix: sqlite-dbs/
```

AWS credentials can be provided via:
- Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
- IAM roles (recommended for EC2/ECS)
- AWS credentials file

### Azure Blob Storage

Store databases in Azure:

```yaml
storage:
  backend: azure
  azure:
    account: myaccount
    container: sqlite-dbs
    use_managed_identity: true
```

For managed identity, ensure your Azure VM/Container has the appropriate role assignments.

Alternatively, use account key:

```yaml
azure:
  account: myaccount
  container: sqlite-dbs
  key: your-account-key
  use_managed_identity: false
```

## Transaction Modes

SQLite supports three transaction modes:

- **DEFERRED** (default): Transaction starts without lock, acquires lock on first write
- **IMMEDIATE**: Acquires reserved lock immediately, preventing other writes
- **EXCLUSIVE**: Acquires exclusive lock immediately

Configure in `config.yaml`:

```yaml
database:
  transaction_mode: deferred  # or immediate, exclusive
```

Or specify per transaction:

```sql
BEGIN IMMEDIATE;
-- Your queries here
COMMIT;
```

## Configuration Reference

### Server Configuration

| Option | Environment Variable | Default | Description |
|--------|---------------------|---------|-------------|
| `server.port` | `PG_PORT` | `5432` | PostgreSQL server port |
| `server.host` | `PG_HOST` | `0.0.0.0` | Server bind address |
| `server.authentication.user` | `PG_USER` | `postgres` | PostgreSQL username |
| `server.authentication.password` | `PG_PASSWORD` | `postgres` | PostgreSQL password |

### Database Configuration

| Option | Environment Variable | Default | Description |
|--------|---------------------|---------|-------------|
| `database.name` | `DB_NAME` | `myapp` | Database name |
| `database.sqlite_path` | `DB_PATH` | `/tmp/myapp.sqlite` | Local SQLite path |
| `database.transaction_mode` | - | `deferred` | Transaction mode |
| `database.connection_pool_size` | `CONNECTION_POOL_SIZE` | `10` | Max connections |

### Storage Configuration

| Option | Environment Variable | Default | Description |
|--------|---------------------|---------|-------------|
| `storage.backend` | `STORAGE` | `local` | Storage backend type |
| `storage.cache_ttl_minutes` | `CACHE_TTL_MINUTES` | `5` | Cache sync interval |

### Logging Configuration

| Option | Environment Variable | Default | Description |
|--------|---------------------|---------|-------------|
| `logging.level` | `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `logging.format` | `LOG_FORMAT` | `text` | Log format (text, json) |

## Docker Deployment

### Build Docker Image

```bash
docker build -t pgserver:latest .
```

### Run with Docker

```bash
# Local storage
docker run -p 5432:5432 \
  -e PG_PASSWORD=secret \
  -e STORAGE=local \
  -v $(pwd)/data:/data \
  pgserver:latest

# S3 storage
docker run -p 5432:5432 \
  -e PG_PASSWORD=secret \
  -e STORAGE=s3 \
  -e S3_BUCKET=my-databases \
  -e S3_REGION=us-east-1 \
  -e AWS_ACCESS_KEY_ID=your-key \
  -e AWS_SECRET_ACCESS_KEY=your-secret \
  pgserver:latest
```

### Docker Compose

```yaml
version: '3.8'
services:
  pgserver:
    image: pgserver:latest
    ports:
      - "5432:5432"
    environment:
      - PG_PASSWORD=secret
      - STORAGE=local
    volumes:
      - ./data:/data
```

## Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pgserver
spec:
  replicas: 1
  selector:
    matchLabels:
      app: pgserver
  template:
    metadata:
      labels:
        app: pgserver
    spec:
      containers:
      - name: pgserver
        image: pgserver:latest
        ports:
        - containerPort: 5432
        env:
        - name: PG_PASSWORD
          valueFrom:
            secretKeyRef:
              name: pgserver-secret
              key: password
        - name: STORAGE
          value: "s3"
        - name: S3_BUCKET
          value: "my-databases"
        - name: S3_REGION
          value: "us-east-1"
        livenessProbe:
          tcpSocket:
            port: 5432
          initialDelaySeconds: 5
          periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: pgserver
spec:
  selector:
    app: pgserver
  ports:
  - port: 5432
    targetPort: 5432
  type: LoadBalancer
```

## Development

### Running Tests

```bash
# Run all tests
go test -v ./...

# Run specific test
go test -v -run TestLocalStorage

# Run with coverage
go test -cover ./...
```

### Building

```bash
# Build for current platform
go build -o pgserver

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o pgserver-linux-amd64

# Build static binary
CGO_ENABLED=1 go build -ldflags="-s -w" -o pgserver
```

## Performance Considerations

### Connection Pooling

Adjust pool size based on workload:

```yaml
database:
  connection_pool_size: 20  # Increase for high concurrency
```

### Cache TTL

Balance between consistency and performance:

```yaml
storage:
  cache_ttl_minutes: 1  # Sync more frequently (higher storage costs)
  cache_ttl_minutes: 60  # Sync less frequently (lower storage costs)
```

### Transaction Mode

- Use **DEFERRED** for mostly-read workloads
- Use **IMMEDIATE** for write-heavy workloads to reduce lock contention
- Use **EXCLUSIVE** when you need guaranteed exclusive access

## Limitations

- **Single Writer**: SQLite's locking model limits concurrent writes
- **Database Size**: Keep databases under 1GB for optimal performance
- **No Streaming Replication**: Changes sync to blob storage asynchronously
- **Limited Concurrency**: Better suited for read-heavy workloads

## Troubleshooting

### Connection Refused

```bash
# Check if server is running
netstat -tlnp | grep 5432

# Check logs
LOG_LEVEL=debug ./pgserver
```

### Authentication Failed

```bash
# Verify credentials
psql -h localhost -p 5432 -U postgres -d myapp

# Check configuration
echo $PG_USER $PG_PASSWORD
```

### Database Locked

If you get "database is locked" errors:

1. Ensure only one server instance per database
2. Use IMMEDIATE transactions for write-heavy workloads
3. Increase timeout in connection string

### Blob Storage Errors

```bash
# Test S3 access
aws s3 ls s3://my-bucket/

# Test Azure access
az storage blob list --account-name myaccount --container-name sqlite-dbs
```

## Contributing

Contributions are welcome! Please open an issue or pull request.

## License

MIT License - see LICENSE file for details.

## Acknowledgments

- [psql-wire](https://github.com/jeroenrinzema/psql-wire) - PostgreSQL wire protocol implementation
- [go-sqlite3](https://github.com/mattn/go-sqlite3) - SQLite driver for Go
- [AWS SDK for Go](https://github.com/aws/aws-sdk-go-v2)
- [Azure SDK for Go](https://github.com/Azure/azure-sdk-for-go)
