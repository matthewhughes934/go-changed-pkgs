.PHONY: coverage.out
coverage.out:
	@go test -race -coverprofile $@ -coverpkg ./... ./...

go-cov.out: coverage.out
	@go run gitlab.com/matthewhughes/go-cov/cmd/go-cov add-skips $^ > go-cov.out

.PHONY: report-coverage
report-coverage: go-cov.out
	@go tool cover -func=$^

.PHONY: report-coverage-html
report-coverage-html: go-cov.out
	@go tool cover -html=$^

.PHONY: check-coverage
check-coverage: go-cov.out
	@go run gitlab.com/matthewhughes/go-cov/cmd/go-cov report --fail-under 100 $^

.PHONY: build
build:
	# --skip=validate to allow for dirty Git state during dev
	# --single-target to only build for the current OS/Arch
	@go run github.com/goreleaser/goreleaser/v2 build --single-target --skip=validate --clean
