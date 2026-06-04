# HTTP Docker Deployment

This document shows the public deployment shape for the `wowdata-server mcp http`
service. Keep production secrets, private key paths, and host-specific config
outside the repository.

## Build Linux Binary

Manual binary builds are optional. Use them only when you need to inspect the
compiled executable outside Docker. A Windows UCRT GCC toolchain can run the
local tests, but it is not a Linux CGO cross compiler.

```bash
mkdir -p dist/linux-amd64
CGO_ENABLED=1 go build -tags wowdata_duckdb -trimpath -ldflags="-s -w" -o dist/linux-amd64/wowdata-server ./cmd/wowdata-server
```

The `wowdata_duckdb` tag is required for HTTP `wow_query` queries. Without it the
service can still start, but DB2 HTTP calls return `query_engine_unavailable`
instead of rows.

## Build Image

`Dockerfile.http` is a multi-stage Dockerfile. It builds the Linux binary in a
Go builder image with CGO enabled and then copies the result into a slim Debian
runtime image. A prebuilt `dist/linux-amd64/wowdata` file is not required.
Run the image build on a Docker host that can build Linux amd64 images.

```bash
docker build -f Dockerfile.http -t wowdata:http-refactor .
```

The private deployment driver under `.local/wowdata/` builds on the remote
Docker CE host and does not require a local Docker installation.

## Container Config

The container command reads `/etc/wowdata/http-mcp.yaml`, so production should
mount a host-specific config file at that path. Inside Docker, bind the service
to `0.0.0.0` so the published host port can reach it. Keep cache and artifact
paths under the mounted container volumes.

```yaml
server:
  host: 0.0.0.0
  port: 9788

cache:
  root: /var/lib/wowdata/cache
  metadata_db: /var/lib/wowdata/cache/metadata.sqlite
  raw_dir: /var/lib/wowdata/cache/raw
  db2_dir: /var/lib/wowdata/cache/db2
  duckdb_path: /var/lib/wowdata/cache/duckdb/wowdata.duckdb

artifacts:
  root: /var/lib/wowdata/artifacts
```

## Run Container

```bash
docker run -d \
  --name wowdata-mcp \
  --restart unless-stopped \
  -p 0.0.0.0:9443:9788 \
  -v /opt/wowdata/config/http-mcp.yaml:/etc/wowdata/http-mcp.yaml:ro \
  -v /opt/wowdata/cache:/var/lib/wowdata/cache \
  -v /opt/wowdata/output:/var/lib/wowdata/artifacts \
  wowdata:http-refactor
```

## Public HTTP Endpoint

The production deployment uses plain HTTP through the provider port mapping:

```text
public 211.154.18.253:11223 -> host 9443 -> Docker 0.0.0.0:9443 -> container 9788
```

Use these public URLs:

- `http://211.154.18.253:11223/mcp`
- `http://211.154.18.253:11223/help`
- `http://211.154.18.253:11223/health`
- `http://211.154.18.253:11223/files/...`
