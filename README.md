# SBS Logger

[![Go Version](https://img.shields.io/badge/Go-1.24.5+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/saviobatista/sbs-logger)](https://goreportcard.com/report/github.com/saviobatista/sbs-logger)
[![Code Coverage](https://codecov.io/gh/saviobatista/sbs-logger/branch/main/graph/badge.svg)](https://codecov.io/gh/saviobatista/sbs-logger)
[![Security Scan](https://github.com/saviobatista/sbs-logger/workflows/CI%2FCD%20Pipeline/badge.svg)](https://github.com/saviobatista/sbs-logger/actions/workflows/ci.yml)
[![Docker Build](https://img.shields.io/badge/Docker-Build%20Passing-brightgreen.svg)](https://github.com/saviobatista/sbs-logger/actions/workflows/ci.yml)
[![Trivy Security](https://img.shields.io/badge/Trivy-Security%20Scan-brightgreen.svg)](https://github.com/saviobatista/sbs-logger/actions/workflows/ci.yml)
[![Go Modules](https://img.shields.io/badge/Go%20Modules-Go%201.24.5+-blue.svg)](go.mod)
[![Dependabot](https://img.shields.io/badge/Dependabot-Enabled-brightgreen.svg)](https://github.com/saviobatista/sbs-logger/security/dependabot)
[![GitHub Issues](https://img.shields.io/github/issues/saviobatista/sbs-logger)](https://github.com/saviobatista/sbs-logger/issues)
[![GitHub Pull Requests](https://img.shields.io/github/issues-pr/saviobatista/sbs-logger)](https://github.com/saviobatista/sbs-logger/pulls)

A high-performance, distributed Go application for capturing, processing, and storing SBS (BaseStation) messages from ADS-B receivers. The system provides real-time aircraft tracking, flight session management, and comprehensive data persistence with TimescaleDB.

## 🚀 Features

- **Real-time SBS Message Ingestion**: Connects to multiple ADS-B receivers simultaneously
- **Distributed Architecture**: Microservices-based design with NATS messaging
- **Aircraft State Tracking**: Real-time position, altitude, speed, and flight data
- **Flight Session Management**: Automatic flight detection and session tracking
- **High-Performance Storage**: TimescaleDB for time-series data with automatic retention policies
- **Redis Caching**: Fast access to active aircraft states and flight data
- **Comprehensive Logging**: Daily log rotation with automatic compression
- **Statistics & Monitoring**: Real-time system metrics and performance tracking
- **Docker Support**: Complete containerized deployment with docker-compose

## 🛡️ Quality & Security

This project maintains high code quality and security standards through automated checks:

- **🔍 Code Quality**: Automated linting with golangci-lint
- **🧪 Testing**: Comprehensive unit and integration tests with race condition detection
- **📊 Code Coverage**: Continuous coverage tracking with Codecov
- **🔒 Security Scanning**: Automated vulnerability scanning with Trivy
- **🐳 Container Security**: Docker image scanning and multi-platform builds
- **📋 Go Report Card**: Code quality analysis and grading
- **🔄 CI/CD**: Automated testing, building, and deployment pipeline

## 🏗️ Architecture

The system consists of several microservices that communicate via NATS:

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Ingestor  │───▶│     NATS    │───▶│   Logger    │
│             │    │             │    │             │
└─────────────┘    └─────────────┘    └─────────────┘
                           │
                           ▼
                   ┌─────────────┐
                   │   Tracker   │
                   │             │
                   └─────────────┘
                           │
                    ┌──────┴──────┐
                    ▼             ▼
            ┌─────────────┐ ┌─────────────┐
            │ TimescaleDB │ │    Redis    │
            │             │ │             │
            └─────────────┘ └─────────────┘
```

### Components

- **Ingestor**: Connects to SBS sources and publishes messages to NATS
- **Logger**: Subscribes to messages and writes to daily log files
- **Tracker**: Processes messages, tracks aircraft states, manages flight sessions, and handles database migrations
- **NATS**: Message broker for inter-service communication
- **TimescaleDB**: Time-series database for aircraft states and statistics
- **Redis**: Caching layer for active aircraft and flight data

## 📋 Prerequisites

- Go 1.24.5 or later
- Docker and Docker Compose
- PostgreSQL/TimescaleDB
- Redis
- NATS Server

## 🛠️ Installation

### Option 1: Docker Compose (Recommended)

1. Clone the repository:
```bash
git clone https://github.com/saviobatista/sbs-logger.git
cd sbs-logger
```

2. Configure environment variables:
```bash
# Copy the sample environment file
cp .env.sample .env

# Edit the .env file with your configuration
nano .env
```

3. Update the key variables in `.env`:
```bash
# Your ADS-B receiver(s)
SOURCES=your-adsb-receiver:30003,another-receiver:30003

# Your receiver location (for Ultrafeeder)
ULTRAFEEDER_LAT=your_latitude
ULTRAFEEDER_LON=your_longitude
ULTRAFEEDER_ALT=your_altitude

# Database credentials (change for production)
POSTGRES_PASSWORD=your_secure_password
```

4. Start the services:
```bash
docker-compose up -d
```

### Option 2: Manual Build

1. Install dependencies:
```bash
go mod download
```

2. Build all components:
```bash
go build ./cmd/ingestor
go build ./cmd/logger
go build ./cmd/tracker
```

3. Start the services:
```bash
# Migrations will run automatically when the tracker service starts
./tracker
```

The tracker service will automatically run database migrations on startup.

## ⚙️ Configuration

### Environment Setup

The project uses a `.env` file for configuration. Start by copying the sample file:

```bash
cp .env.sample .env
```

### Environment Variables

#### Ingestor
- `SOURCES`: Comma-separated list of SBS sources (e.g., `10.0.0.1:30003,10.0.0.2:30003`)
- `NATS_URL`: NATS server URL (default: `nats://nats:4222`)

#### Logger
- `OUTPUT_DIR`: Directory for log files (default: `./logs`)
- `NATS_URL`: NATS server URL (default: `nats://nats:4222`)

#### Tracker
- `NATS_URL`: NATS server URL (default: `nats://nats:4222`)
- `DB_CONN_STR`: Database connection string
- `REDIS_ADDR`: Redis server address (default: `redis:6379`)

### Environment Variables Organization

The `.env.sample` file is organized into sections:

- **Ultrafeeder Configuration**: ADS-B receiver settings and web interface
- **SBS Services**: Configuration for ingestor, logger, and tracker
- **Database Configuration**: TimescaleDB connection settings
- **Security**: Optional authentication and SSL settings

### Database Schema

The system uses TimescaleDB with the following main tables:

- `aircraft_states`: Time-series table for aircraft position and state data
- `flights`: Flight session information
- `system_stats`: System performance and statistics

### NATS Configuration

NATS is configured with JetStream enabled for message persistence:

```conf
port: 4222
http_port: 8222

jetstream {
    store_dir: "/data"
    max_memory_store: 1G
    max_file_store: 10G
}
```

## 📊 Data Processing

### SBS Message Types

The system processes the following SBS message types:

- **MSG,1**: Selection change
- **MSG,2**: New aircraft
- **MSG,3**: New ID
- **MSG,4**: New callsign
- **MSG,5**: New altitude
- **MSG,6**: New ground speed
- **MSG,7**: New track
- **MSG,8**: New lat/lon (position)
- **MSG,9**: New ground status

### Aircraft State Tracking

The tracker maintains real-time state for each aircraft:

- Position (latitude/longitude)
- Altitude and vertical rate
- Ground speed and track
- Callsign and squawk
- Ground status

### Flight Sessions

A flight session is one continuous period in which an aircraft (ICAO hex
ident) is heard:

- **Start**: the first message of an aircraft that has no active flight
  creates a row in `flights` with a new `session_id` (UUID) and
  `started_at` = the message time.
- **Updates**: the callsign, the first and last known position, the maximum
  altitude and the maximum ground speed seen, and `last_seen_at`. The row is
  written when the flight starts, at most once a minute while it is active,
  when it ends and when the tracker shuts down.
- **End**: when the aircraft has been silent for more than 5 minutes,
  `ended_at` = `last_seen_at`. A message after such a gap starts a new session.
- **Time** is the message (ingest) time, not the wall clock, so a tracker that
  runs behind the stream still measures the gaps correctly.
- **Restarts**: on startup the tracker resumes the flights with
  `ended_at IS NULL` and closes the ones that went silent meanwhile. A partial
  unique index allows one active flight per aircraft.

### NATS consumers

The logger and the tracker read `sbs.raw` through durable pull consumers named
`sbs-logger` and `sbs-tracker`, in batches. A consumer is created on the first
start with the messages published from then on, and every restart resumes
after the last acknowledged message. Messages are acknowledged after they were
written (logger) or stored (tracker); a failed batch is redelivered after 5 s.

### Log files

The logger writes `OUTPUT_DIR/sbs_YYYY-MM-DD.log` (UTC day), one SBS message
per line terminated by `\n`, and gzips the previous day on rotation
(`sbs_YYYY-MM-DD.log.gz`).

Files written before this format was fixed have the day's messages
concatenated on one line, and their `.log.gz` are plain text despite the
name. `cmd/resplit` restores one message per line (plain or gzip input, it
never overwrites an existing output file):

```bash
go run ./cmd/resplit -o sbs_2026-09-23.split.log sbs_2026-09-23.log.gz
```

### Metrics

Each service serves Prometheus metrics at `/metrics`. `METRICS_ADDR`
overrides the listen address (an empty value disables it).

| Service  | Default port | Series |
|----------|--------------|--------|
| ingestor | 9101 | `sbs_ingestor_messages_total`, `sbs_ingestor_bytes_total`, `sbs_ingestor_reconnects_total`, `sbs_ingestor_publish_errors_total`, `sbs_ingestor_connected` (label `source`) |
| logger   | 9102 | `sbs_logger_messages_written_total`, `sbs_logger_bytes_written_total`, `sbs_logger_write_errors_total`, `sbs_logger_lag_seconds` |
| tracker  | 9103 | `sbs_tracker_messages_total`, `sbs_tracker_messages_parsed_total`, `sbs_tracker_messages_failed_total`, `sbs_tracker_states_stored_total`, `sbs_tracker_flights_created_total`, `sbs_tracker_flights_ended_total`, `sbs_tracker_flights_active`, `sbs_tracker_aircraft_active`, `sbs_tracker_lag_seconds` |

## 📈 Monitoring & Statistics

The system provides comprehensive statistics:

- Message processing rates
- Aircraft and flight counts
- Processing performance metrics
- Error rates and system health

Statistics are logged every minute and persisted to the database every 5 minutes.

## 🔧 Development

### Project Structure

```
sbs-logger/
├── cmd/                    # Application entry points
│   ├── ingestor/          # SBS message ingestion
│   ├── logger/            # Log file management
│   └── tracker/           # Aircraft state tracking
├── internal/              # Private application code
│   ├── capture/           # Network capture logic
│   ├── config/            # Configuration management
│   ├── db/                # Database operations
│   ├── nats/              # NATS client
│   ├── parser/            # SBS message parsing
│   ├── redis/             # Redis client
│   ├── stats/             # Statistics tracking
│   ├── storage/           # Storage abstractions
│   └── types/             # Data structures
├── config/                # Configuration files
│   └── nats/              # NATS server configuration
├── logs/                  # Log file output
└── docker-compose.yml     # Container orchestration
```

### Running Tests

```bash
go test ./...
```

### Building for Production

```bash
# Build all components
make build

# Build individual components
make build-ingestor
make build-logger
make build-tracker
```

### Docker Hub Publishing

The project includes automated Docker Hub publishing. To set up:

1. **Setup Docker Hub publishing**:
```bash
make dockerhub-setup
```

2. **Test Docker builds locally**:
```bash
make dockerhub-test
```

3. **Manual push to Docker Hub**:
```bash
make dockerhub-push DOCKERHUB_USERNAME=youruser DOCKERHUB_TOKEN=yourtoken VERSION=v1.0.0
```

4. **Automated publishing**: Create a GitHub release to trigger automatic publishing

For detailed setup instructions, see [Docker Hub Setup Guide](docs/dockerhub-setup.md).

## 🚀 Deployment

### Docker Images

The project provides pre-built Docker images on multiple registries:

#### GitHub Container Registry (GHCR)
```bash
# Pull images from GHCR
docker pull ghcr.io/saviobatista/sbs-logger/sbs-ingestor:latest
docker pull ghcr.io/saviobatista/sbs-logger/sbs-logger:latest
docker pull ghcr.io/saviobatista/sbs-logger/sbs-tracker:latest
```

#### Docker Hub
```bash
# Pull images from Docker Hub
docker pull saviobatista/sbs-ingestor:latest
docker pull saviobatista/sbs-logger:latest
docker pull saviobatista/sbs-tracker:latest
```

### Production Considerations

1. **Scaling**: Run multiple ingestor instances for high availability
2. **Storage**: Configure appropriate retention policies for TimescaleDB
3. **Monitoring**: Set up monitoring for NATS, Redis, and TimescaleDB
4. **Backup**: Implement regular database backups
5. **Security**: Use TLS for NATS and database connections

### Docker Deployment

```bash
# Production deployment
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# Scale services
docker-compose up -d --scale ingestor=3
```

### Using Pre-built Images

Update your `docker-compose.yml` to use pre-built images:

```yaml
services:
  ingestor:
    image: saviobatista/sbs-ingestor:latest
    # or: image: ghcr.io/saviobatista/sbs-logger/sbs-ingestor:latest
    environment:
      - SOURCES=your-adsb-receiver:30003
      - NATS_URL=nats://nats:4222
    depends_on:
      - nats

  logger:
    image: saviobatista/sbs-logger:latest
    environment:
      - OUTPUT_DIR=/app/logs
      - NATS_URL=nats://nats:4222
    volumes:
      - ./logs:/app/logs
    depends_on:
      - nats
```

## 📝 Logging

Logs are written to daily files with automatic rotation:

- Format: `sbs_YYYY-MM-DD.log`
- Compression: Previous day's logs are automatically compressed
- Location: `./logs/` directory (configurable)

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## 📄 License

MIT License - see LICENSE file for details.

## 🆘 Support

For issues and questions:

1. Check the documentation
2. Search existing issues
3. Create a new issue with detailed information

## 🔗 Related Projects

- [ADS-B Exchange](https://www.adsbexchange.com/)
- [FlightAware](https://flightaware.com/)
- [OpenSky Network](https://opensky-network.org/) 