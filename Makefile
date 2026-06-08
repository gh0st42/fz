BINARY  := fz
DIST    := dist/release
VERSION := $(shell sed -n 's/.*version = "\(.*\)"/\1/p' version.go)

NATIVE_OS   := $(shell go env GOOS)
NATIVE_ARCH := $(shell go env GOARCH)
ZIG         := $(shell which zig 2>/dev/null)
DOCKER      := $(shell which docker 2>/dev/null)

PLATFORMS := \
	darwin/arm64 \
	linux/arm64 \
	linux/amd64 \
	windows/amd64

# Debian packages required by raylib-go's GLFW (X11 + OpenGL).
LINUX_DEPS := libx11-dev libxrandr-dev libxi-dev libxcursor-dev \
              libxinerama-dev libxkbcommon-dev libgl1-mesa-dev libwayland-dev

.PHONY: build release clean install

build:
	go build -o $(BINARY) .

release:
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		out=$(DIST)/$(BINARY)-$(VERSION)-$$os-$$arch; \
		if [ "$$os" = "windows" ]; then out=$$out.exe; fi; \
		\
		if [ "$$os/$$arch" = "$(NATIVE_OS)/$(NATIVE_ARCH)" ]; then \
			echo "Building (native) $$out ..."; \
			CGO_ENABLED=1 GOOS=$$os GOARCH=$$arch go build -o $$out . || exit 1; \
		\
		elif [ "$$os" = "linux" ] && [ "$(NATIVE_OS)" != "linux" ]; then \
			if [ -z "$(DOCKER)" ]; then \
				echo "Skipping $$os/$$arch — install Docker to cross-compile for Linux"; \
			else \
				echo "Building (docker linux/$$arch) $$out ..."; \
				docker run --rm \
					--platform $$os/$$arch \
					-v "$$(pwd)":/src \
					-w /src \
					golang:1.26-bookworm \
					bash -c "apt-get -o Acquire::Check-Valid-Until=false -o Acquire::Check-Date=false \
					                  -o Acquire::AllowInsecureRepositories=true update -qq; \
					         apt-get install -y -qq --no-install-recommends --allow-unauthenticated $(LINUX_DEPS) && \
					         mkdir -p $$(dirname $$out) && \
					         CGO_ENABLED=1 go build -o $$out ." || exit 1; \
			fi; \
		\
		elif [ -n "$(ZIG)" ]; then \
			case "$$os/$$arch" in \
				darwin/arm64)  zig_triple=aarch64-macos       ;; \
				darwin/amd64)  zig_triple=x86_64-macos        ;; \
				linux/arm64)   zig_triple=aarch64-linux-musl  ;; \
				linux/amd64)   zig_triple=x86_64-linux-musl   ;; \
				windows/arm64) zig_triple=aarch64-windows-gnu ;; \
				windows/amd64) zig_triple=x86_64-windows-gnu  ;; \
			esac; \
			echo "Building (zig $$zig_triple) $$out ..."; \
			CC="$(ZIG) cc -target $$zig_triple" \
			CGO_ENABLED=1 GOOS=$$os GOARCH=$$arch \
			go build -o $$out . || exit 1; \
		\
		else \
			echo "Skipping $$os/$$arch — install zig (non-Linux) or docker (Linux)"; \
		fi; \
	done
	@echo "Done. Binaries in $(DIST)/"

install:
	go install .

clean:
	rm -f $(BINARY)
	rm -rf $(DIST)
