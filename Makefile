APP_PKG := github.com/MeguruMacabre/MeguruPacks/internal/appconfig

-include secrets.host.mk
-include secrets.client.mk

HOST_OUT_DIR ?= ./dist/host
CLIENT_OUT_DIR ?= ./dist/client

HOST_BIN_NAME ?= megurupacks-host
CLIENT_BIN_NAME ?= megurupacks-client

HOST_S3_BUCKET ?= your-bucket
HOST_S3_REGION ?= ru-1
HOST_S3_ENDPOINT ?= https://s3.example.com
HOST_S3_PATH_STYLE ?= false
HOST_S3_PREFIX ?= megurupacks
HOST_S3_ACCESS_KEY_ID ?= REPLACE_ME
HOST_S3_SECRET_KEY ?= REPLACE_ME
HOST_S3_CAPACITY_GB ?= 10

CLIENT_S3_BUCKET ?= your-bucket
CLIENT_S3_REGION ?= ru-1
CLIENT_S3_ENDPOINT ?= https://s3.example.com
CLIENT_S3_PATH_STYLE ?= false
CLIENT_S3_PREFIX ?= megurupacks
CLIENT_S3_ACCESS_KEY_ID ?=
CLIENT_S3_SECRET_KEY ?=
CLIENT_S3_CAPACITY_GB ?= 10

HOST_LDFLAGS := \
-X '$(APP_PKG).S3Bucket=$(HOST_S3_BUCKET)' \
-X '$(APP_PKG).S3Region=$(HOST_S3_REGION)' \
-X '$(APP_PKG).S3Endpoint=$(HOST_S3_ENDPOINT)' \
-X '$(APP_PKG).S3PathStyle=$(HOST_S3_PATH_STYLE)' \
-X '$(APP_PKG).S3Prefix=$(HOST_S3_PREFIX)' \
-X '$(APP_PKG).S3AccessKeyID=$(HOST_S3_ACCESS_KEY_ID)' \
-X '$(APP_PKG).S3SecretKey=$(HOST_S3_SECRET_KEY)' \
-X '$(APP_PKG).S3CapacityGB=$(HOST_S3_CAPACITY_GB)'

CLIENT_LDFLAGS := \
-X '$(APP_PKG).S3Bucket=$(CLIENT_S3_BUCKET)' \
-X '$(APP_PKG).S3Region=$(CLIENT_S3_REGION)' \
-X '$(APP_PKG).S3Endpoint=$(CLIENT_S3_ENDPOINT)' \
-X '$(APP_PKG).S3PathStyle=$(CLIENT_S3_PATH_STYLE)' \
-X '$(APP_PKG).S3Prefix=$(CLIENT_S3_PREFIX)' \
-X '$(APP_PKG).S3AccessKeyID=$(CLIENT_S3_ACCESS_KEY_ID)' \
-X '$(APP_PKG).S3SecretKey=$(CLIENT_S3_SECRET_KEY)' \
-X '$(APP_PKG).S3CapacityGB=$(CLIENT_S3_CAPACITY_GB)'

.PHONY: help clean \
	build-host build-client build-all \
	build-host-all build-client-all \
	build-host-darwin-amd64 build-host-darwin-arm64 build-host-linux-amd64 build-host-linux-arm64 build-host-windows-amd64 \
	build-client-darwin-amd64 build-client-darwin-arm64 build-client-linux-amd64 build-client-linux-arm64 build-client-windows-amd64

help:
	@echo "MeguruPacks build targets:"
	@echo "  make build-host"
	@echo "  make build-client"
	@echo "  make build-all"
	@echo "  make build-host-all"
	@echo "  make build-client-all"
	@echo "  make clean"

clean:
	rm -rf ./dist

build-host: build-host-$(shell go env GOOS)-$(shell go env GOARCH)
build-client: build-client-$(shell go env GOOS)-$(shell go env GOARCH)
build-all: build-host-all build-client-all

build-host-all: \
	build-host-darwin-amd64 \
	build-host-darwin-arm64 \
	build-host-linux-amd64 \
	build-host-linux-arm64 \
	build-host-windows-amd64

build-client-all: \
	build-client-darwin-amd64 \
	build-client-darwin-arm64 \
	build-client-linux-amd64 \
	build-client-linux-arm64 \
	build-client-windows-amd64

define build_binary
	@mkdir -p $(3)
	CGO_ENABLED=0 GOOS=$(1) GOARCH=$(2) go build -trimpath -ldflags "$(4)" -o "$(3)/$(5)$(if $(filter windows,$(1)),.exe,)" $(6)
endef

build-host-darwin-amd64:
	$(call build_binary,darwin,amd64,$(HOST_OUT_DIR),$(HOST_LDFLAGS),$(HOST_BIN_NAME)-darwin-amd64,./cmd/host)

build-host-darwin-arm64:
	$(call build_binary,darwin,arm64,$(HOST_OUT_DIR),$(HOST_LDFLAGS),$(HOST_BIN_NAME)-darwin-arm64,./cmd/host)

build-host-linux-amd64:
	$(call build_binary,linux,amd64,$(HOST_OUT_DIR),$(HOST_LDFLAGS),$(HOST_BIN_NAME)-linux-amd64,./cmd/host)

build-host-linux-arm64:
	$(call build_binary,linux,arm64,$(HOST_OUT_DIR),$(HOST_LDFLAGS),$(HOST_BIN_NAME)-linux-arm64,./cmd/host)

build-host-windows-amd64:
	$(call build_binary,windows,amd64,$(HOST_OUT_DIR),$(HOST_LDFLAGS),$(HOST_BIN_NAME)-windows-amd64,./cmd/host)

build-client-darwin-amd64:
	$(call build_binary,darwin,amd64,$(CLIENT_OUT_DIR),$(CLIENT_LDFLAGS),$(CLIENT_BIN_NAME)-darwin-amd64,./cmd/client)

build-client-darwin-arm64:
	$(call build_binary,darwin,arm64,$(CLIENT_OUT_DIR),$(CLIENT_LDFLAGS),$(CLIENT_BIN_NAME)-darwin-arm64,./cmd/client)

build-client-linux-amd64:
	$(call build_binary,linux,amd64,$(CLIENT_OUT_DIR),$(CLIENT_LDFLAGS),$(CLIENT_BIN_NAME)-linux-amd64,./cmd/client)

build-client-linux-arm64:
	$(call build_binary,linux,arm64,$(CLIENT_OUT_DIR),$(CLIENT_LDFLAGS),$(CLIENT_BIN_NAME)-linux-arm64,./cmd/client)

build-client-windows-amd64:
	$(call build_binary,windows,amd64,$(CLIENT_OUT_DIR),$(CLIENT_LDFLAGS),$(CLIENT_BIN_NAME)-windows-amd64,./cmd/client)