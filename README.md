# SBS Logger

A Go-based application that captures SBS (BaseStation) messages from network devices and stores them in local files.

## Features

- Network message capture for SBS protocol
- Local file storage of captured messages
- Configurable logging options
- Real-time message processing

## Project Structure

```
.
├── cmd/            # Command-line interface
├── internal/       # Private application code
│   ├── capture/    # Network capture logic
│   ├── parser/     # SBS message parsing
│   └── storage/    # File storage implementation
├── pkg/            # Public library code
└── config/         # Configuration files
```

## Setup

1. Ensure you have Go 1.21 or later installed
2. Clone the repository
3. Run `go mod download` to install dependencies
4. Build the project with `go build`

## Usage

[Usage instructions will be added as the project develops]

## License

MIT License 