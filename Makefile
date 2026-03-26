GO          ?= go
PKG         ?= ./daemon
OUT_DIR     ?= build
BASENAME    ?= automagicd

.PHONY: build-386 build-armv7 build-arm64 build-all build-host
build-all: build-host build-386 build-armv7 build-arm64

build-host: $(OUT_DIR)/$(BASENAME)

$(OUT_DIR)/$(BASENAME):
	mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 $(GO) build -o $@ $(PKG)


build-386:   $(OUT_DIR)/$(BASENAME)-386
build-armv7: $(OUT_DIR)/$(BASENAME)-armv7
build-arm64: $(OUT_DIR)/$(BASENAME)-arm64


$(OUT_DIR)/$(BASENAME)-386:
	mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=386 CGO_ENABLED=0 $(GO) build -o $@ $(PKG)

$(OUT_DIR)/$(BASENAME)-armv7:
	mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 $(GO) build -o $@ $(PKG)

$(OUT_DIR)/$(BASENAME)-arm64:
	mkdir -p $(OUT_DIR)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o $@ $(PKG)
