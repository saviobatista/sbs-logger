# SBS Logger

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
- **Tracker**: Processes messages, tracks aircraft states, and manages flight sessions
- **Migrate**: Database schema management and migrations
- **NATS**: Message broker for inter-service communication
- **TimescaleDB**: Time-series database for aircraft states and statistics
- **Redis**: Caching layer for active aircraft and flight data

## 📋 Prerequisites

- Go 1.23.0 or later
- Docker and Docker Compose
- PostgreSQL/TimescaleDB
- Redis
- NATS Server

## 🛠️ Installation

### Option 1: Docker Compose (Recommended)

1. Clone the repository:
```bash
git clone https://github.com/savio/sbs-logger.git
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
go build ./cmd/migrate
```

3. Set up the database:
```bash
# Run migrations
./migrate -db="postgres://user:pass@localhost:5432/sbs_data?sslmode=disable"
```

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

#### Migrate
- `DB_CONN_STR`: Database connection string

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

Flight sessions are automatically detected and tracked:

- Session start/end times
- Flight path (first/last position)
- Maximum altitude and speed
- Session statistics

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
│   ├── tracker/           # Aircraft state tracking
│   └── migrate/           # Database migrations
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

## 🚀 Deployment

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