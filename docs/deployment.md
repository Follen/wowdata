# HTTP Docker Deployment

This document shows the public deployment shape for the `wowdata mcp http`
service. Keep production secrets, private key paths, and host-specific config
outside the repository.

## Build Linux Binary

Build the Linux binary on a Linux Docker host or another Linux build
environment with a working CGO compiler. A Windows UCRT GCC toolchain can run
the local tests, but it is not a Linux CGO cross compiler.

```bash
mkdir -p dist/linux-amd64
CGO_ENABLED=1 go build -tags wowdata_duckdb -trimpath -ldflags="-s -w" -o dist/linux-amd64/wowdata ./cmd/wowdata
```

The `wowdata_duckdb` tag is required for HTTP `wow_db2` queries. Without it the
service can still start, but DB2 HTTP calls return `query_engine_unavailable`
instead of rows.

## Build Image

Run the image build in the same Linux-capable environment after the binary is
created.

```bash
docker build -f Dockerfile.http -t wowdata:http-refactor .
```

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
  -p 127.0.0.1:9788:9788 \
  -v /opt/wowdata/config/http-mcp.yaml:/etc/wowdata/http-mcp.yaml:ro \
  -v /opt/wowdata/cache:/var/lib/wowdata/cache \
  -v /opt/wowdata/output:/var/lib/wowdata/artifacts \
  wowdata:http-refactor
```

## Nginx Proxy

Public traffic should be proxied from `211.154.18.253:11224` to the local
container listener at `127.0.0.1:9788`.

```nginx
server {
    listen 11224;
    server_name 211.154.18.253;

    client_max_body_size 25m;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;

    location = /mcp {
        proxy_pass http://127.0.0.1:9788;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location ^~ /mcp/ {
        proxy_pass http://127.0.0.1:9788;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /health {
        proxy_pass http://127.0.0.1:9788;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```
