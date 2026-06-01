# HTTP Docker Deployment

This document shows the public deployment shape for the `wowdata mcp http`
service. Keep production secrets, private key paths, and host-specific config
outside the repository.

## Build Linux Binary

```powershell
$GO = "C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe"
$oldPath = $env:PATH
$oldGoos = $env:GOOS
$oldGoarch = $env:GOARCH
$oldCgo = $env:CGO_ENABLED
$oldCc = $env:CC
$oldCxx = $env:CXX

try {
    $env:PATH = "C:\msys64\ucrt64\bin;$oldPath"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    $env:CGO_ENABLED = "1"
    $env:CC = "gcc"
    $env:CXX = "g++"

    New-Item -ItemType Directory -Force -Path dist/linux-amd64 | Out-Null
    & $GO build -trimpath -ldflags="-s -w" -o dist/linux-amd64/wowdata ./cmd/wowdata
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
}
finally {
    $env:PATH = $oldPath
    if ($null -eq $oldGoos) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $oldGoos }
    if ($null -eq $oldGoarch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $oldGoarch }
    if ($null -eq $oldCgo) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $oldCgo }
    if ($null -eq $oldCc) { Remove-Item Env:CC -ErrorAction SilentlyContinue } else { $env:CC = $oldCc }
    if ($null -eq $oldCxx) { Remove-Item Env:CXX -ErrorAction SilentlyContinue } else { $env:CXX = $oldCxx }
}
```

## Build Image

```powershell
docker build -f Dockerfile.http -t wowdata:http-refactor .
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
