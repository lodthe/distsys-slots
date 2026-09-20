.PHONY: check test browser-check e2e-check security-check up
check: bin/staticcheck
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	bin/staticcheck ./...
	go test -race ./...
	go build ./...
	@for script in deploy/*.sh tests/*.sh tests/browser/*.sh; do sh -n "$$script"; done
test:
	go test -race ./...
up:
	docker compose up -d --build

browser-check:
	docker build -t distsys-browser-check tests/browser
	docker run --rm --network none -v "$(CURDIR):/work:ro" distsys-browser-check sh tests/browser/check.sh

e2e-check:
	sh tests/e2e.sh

security-check: bin/gitleaks bin/govulncheck
	sh tests/secrets.sh
	bin/govulncheck ./...

bin/staticcheck:
	GOBIN="$(CURDIR)/bin" go install honnef.co/go/tools/cmd/staticcheck@v0.8.1

bin/gitleaks:
	GOBIN="$(CURDIR)/bin" go install github.com/zricethezav/gitleaks/v8@v8.30.1

bin/govulncheck:
	GOBIN="$(CURDIR)/bin" go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
