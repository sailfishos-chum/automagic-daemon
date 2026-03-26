GO          ?= go
PKG         ?= ./cbindings
OUT_DIR     ?= build
LIB_BASENAME?= automagicd

CC_386      ?= i686-linux-gnu-gcc
CC_ARMV7    ?= arm-linux-gnueabihf-gcc
CC_ARM64    ?= aarch64-linux-gnu-gcc

.PHONY: test
test:
	go test ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: build-lib
build-lib: $(OUT_DIR)/$(LIB_BASENAME).so

$(OUT_DIR)/$(LIB_BASENAME).so:
	mkdir -p $(OUT_DIR)
	CGO_ENABLED=1 $(GO) build -buildmode=c-shared -o $@ $(PKG)

.PHONY: build-lib-386 build-lib-armv7 build-lib-arm64 build-lib-all

build-lib-386:   $(OUT_DIR)/$(LIB_BASENAME)-386.so
build-lib-armv7: $(OUT_DIR)/$(LIB_BASENAME)-armv7.so
build-lib-arm64: $(OUT_DIR)/$(LIB_BASENAME)-arm64.so

build-lib-all: build-lib build-lib-386 build-lib-armv7 build-lib-arm64

$(OUT_DIR)/$(LIB_BASENAME)-386.so:
	mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=386 CGO_ENABLED=1 CC=$(CC_386) $(GO) build -buildmode=c-shared -o $@ $(PKG)

$(OUT_DIR)/$(LIB_BASENAME)-armv7.so:
	mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 CC=$(CC_ARMV7) $(GO) build -buildmode=c-shared -o $@ $(PKG)

$(OUT_DIR)/$(LIB_BASENAME)-arm64.so:
	mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=$(CC_ARM64) $(GO) build -buildmode=c-shared -o $@ $(PKG)
