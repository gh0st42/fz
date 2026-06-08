BINARY  := fz
DIST    := dist/release
VERSION := $(shell sed -n 's/.*version = "\(.*\)"/\1/p' version.go)

PLATFORMS := \
	darwin/arm64 \
	darwin/amd64 \
	linux/arm64 \
	linux/amd64 \
	windows/arm64 \
	windows/amd64

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
		echo "Building $$out ..."; \
		GOOS=$$os GOARCH=$$arch go build -o $$out . || exit 1; \
	done
	@echo "All binaries written to $(DIST)/"

install:
	go install .

clean:
	rm -f $(BINARY)
	rm -rf $(DIST)
