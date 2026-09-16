# agent-handoff — build the cross-platform handoff binary (pure Go stdlib, no deps)
BINDIR := bin
TOOL := tool
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build test selfcheck clean install

all: build

build:
	@mkdir -p $(BINDIR)
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; ext=""; \
	  [ "$$os" = "windows" ] && ext=".exe"; \
	  echo "building $$os/$$arch"; \
	  (cd $(TOOL) && CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags="-s -w" \
	    -o ../$(BINDIR)/handoff-$$os-$$arch$$ext .) || exit 1; \
	done
	@(cd $(TOOL) && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../$(BINDIR)/handoff .)

test:
	(cd $(TOOL) && go vet . && go test .)
	(cd $(TOOL) && go build -o /tmp/handoff-test . && /tmp/handoff-test selfcheck)

selfcheck: build
	$(BINDIR)/handoff selfcheck

install: build
	cp $(BINDIR)/handoff $${HOME}/.local/bin/handoff

clean:
	rm -rf $(BINDIR)
