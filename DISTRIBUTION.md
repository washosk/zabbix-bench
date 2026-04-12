# Distribution Guide

This document covers distributing zabbix-bench through various package managers.

---

## Homebrew (macOS/Linux)

### Step 1: Create a Homebrew tap repository

```bash
# Create a new repository: homebrew-zabbix-bench
gh repo create homebrew-zabbix-bench \
  --public \
  --description "Homebrew tap for zabbix-bench"
```

### Step 2: Create the Homebrew formula

File: `Formula/zabbix-bench.rb`

```ruby
class ZabbixBench < Formula
  desc "High-performance Zabbix NVPS benchmark tool"
  homepage "https://github.com/washosk/zabbix-bench"
  url "https://github.com/washosk/zabbix-bench/releases/download/v1.4.0/zabbix-bench-linux-amd64"
  sha256 "$(sha256sum zabbix-bench | cut -d' ' -f1)"
  license "MIT"

  depends_on "go" => :build

  def install
    bin.install "zabbix-bench"
  end

  test do
    system "#{bin}/zabbix-bench", "--help"
  end
end
```

### Step 3: Update on each release

```bash
# Get SHA256 of the released binary
curl -L https://github.com/washosk/zabbix-bench/releases/download/vX.Y.Z/zabbix-bench | sha256sum

# Update Formula with new version and SHA256
# Commit and push
git commit -am "zabbix-bench vX.Y.Z"
git push origin main
```

### Step 4: Install via Homebrew

Users can then install with:

```bash
brew tap washosk/zabbix-bench
brew install zabbix-bench
```

---

## AUR (Arch User Repository)

### Step 1: Create AUR account

- Visit <https://aur.archlinux.org/account-edit/>
- Create SSH key for AUR access

### Step 2: Clone AUR repository

```bash
git clone ssh://aur@aur.archlinux.org/zabbix-bench.git
cd zabbix-bench
```

### Step 3: Create PKGBUILD

File: `PKGBUILD`

```bash
pkgname=zabbix-bench
pkgver=1.0.0
pkgrel=1
pkgdesc="High-performance Zabbix NVPS benchmark tool"
arch=('x86_64' 'aarch64')
url="https://github.com/washosk/zabbix-bench"
license=('MIT')
makedepends=('go>=1.24')
depends=()
source=("$pkgname-$pkgver.tar.gz::https://github.com/washosk/zabbix-bench/archive/refs/tags/v$pkgver.tar.gz")
sha256sums=('SKIP')

build() {
    cd "$pkgname-$pkgver"
    go build -o zabbix-bench main.go
}

package() {
    cd "$pkgname-$pkgver"
    install -Dm755 zabbix-bench "$pkgdir/usr/bin/zabbix-bench"
    install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
    install -Dm644 README.md "$pkgdir/usr/share/doc/$pkgname/README.md"
}
```

### Step 4: Create .SRCINFO

```bash
makepkg --printsrcinfo > .SRCINFO
```

### Step 5: Publish to AUR

```bash
git add -A
git commit -m "zabbix-bench vX.Y.Z"
git push origin main
```

Users install with:

```bash
yay -S zabbix-bench
```

---

## Go Package Registry

### Publish to pkg.go.dev

The tool is automatically indexed at <https://pkg.go.dev/github.com/washosk/zabbix-bench>

Users can install with:

```bash
go install github.com/washosk/zabbix-bench/cmd/zabbix-bench@latest
```

---

## Docker

### Step 1: Create Dockerfile

```dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o zabbix-bench main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /build/zabbix-bench /usr/local/bin/
ENTRYPOINT ["zabbix-bench"]
```

### Step 2: Build and push

```bash
docker build -t washosk/zabbix-bench:latest .
docker tag washosk/zabbix-bench:latest washosk/zabbix-bench:v1.4.0
docker push washosk/zabbix-bench:latest
docker push washosk/zabbix-bench:v1.4.0
```

### Step 3: Create docker-compose example

File: `docker-compose.yml`

```yaml
version: '3.8'
services:
  zabbix-bench:
    image: washosk/zabbix-bench:latest
    environment:
      ZABBIX_API_KEY: ${ZABBIX_API_KEY}
    command: >
      -api-url http://zabbix:8080/api_jsonrpc.php
      -hosts 50
      -senders 20
      -duration 10m
      -output-json /results/benchmark.json
    volumes:
      - ./results:/results
```

Users run with:

```bash
docker run -e ZABBIX_API_KEY=token washosk/zabbix-bench \
  -api-url http://localhost:8080/api_jsonrpc.php -hosts 20
```

---

## Linux Distributions

### Debian/Ubuntu (APT)

Create a PPA or deb repository with:

```bash
# Build deb package
dpkg-deb --build zabbix-bench_1.0.0_amd64 zabbix-bench_1.0.0_amd64.deb

# Host on repository server
# Users: apt-add-repository ppa:yourname/zabbix-bench
# apt update && apt install zabbix-bench
```

### RPM/Fedora

```bash
# Build RPM spec file
# Build: rpmbuild -bb zabbix-bench.spec
# Users: dnf install zabbix-bench
```

---

## Distribution Checklist

- [ ] GitHub releases with binaries for all platforms
- [ ] Homebrew formula created and published
- [ ] AUR package submitted
- [ ] Docker image built and pushed
- [ ] Documentation updated with install instructions
- [ ] GitHub Actions workflows verified
- [ ] Release notes written for each version
- [ ] Changelog maintained

---

## Maintenance

### For each new release

1. Tag commit: `git tag vX.Y.Z && git push origin vX.Y.Z`
2. Create GitHub release with all binaries
3. Update Homebrew formula with new version/SHA
4. Update AUR PKGBUILD and .SRCINFO
5. Build and push Docker image
6. Update installation docs in main README

### Version strategy

- Use semantic versioning (major.minor.patch)
- Tag releases with `v` prefix: `v1.0.0`
- Update all distribution channels simultaneously
