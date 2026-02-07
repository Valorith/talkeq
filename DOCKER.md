# Docker Deployment Guide

## Quick Start

1. **Create your config file:**
   ```bash
   # Run once to generate a default talkeq.conf, then edit it
   go run . 
   # Or copy an existing talkeq.conf into this directory
   ```

2. **Copy the example env file:**
   ```bash
   cp .env.example .env
   ```

3. **Build and run:**
   ```bash
   docker compose up -d
   ```

4. **View logs:**
   ```bash
   docker compose logs -f talkeq
   ```

## Building Manually

```bash
docker build -t talkeq .
docker run -v $(pwd)/talkeq.conf:/app/talkeq.conf:ro talkeq
```

## Configuration

TalkEQ reads its configuration from `talkeq.conf` (TOML format), which is mounted into the container as a read-only volume.

Edit `talkeq.conf` on the host and restart the container to apply changes:

```bash
docker compose restart talkeq
```

## Networking

If TalkEQ needs to connect to an EQEmu server via telnet on the host machine, uncomment `network_mode: host` in `docker-compose.yml`.

Otherwise, ensure the telnet host in `talkeq.conf` points to an address reachable from the container (e.g., `host.docker.internal` on Docker Desktop, or the host's LAN IP).

## Stopping

```bash
docker compose down
```
