COMMIT_SHA_SHORT ?= $(shell git rev-parse --short=12 HEAD)
PWD_DIR := ${CURDIR}

default: help

#==========================================================================================
##@ Testing
#==========================================================================================
test: ## run fast go tests
	@go test ./... -cover

test-integration: ## run the Postgres tests in Docker (integration module)
	@cd integration && TESTDBS_ALL=1 go test -count=1 ./...

test-all: test test-integration ## run all tests incl. Postgres in Docker

lint: ## run go linter
	# depends on https://github.com/golangci/golangci-lint
	@golangci-lint run
	@cd integration && golangci-lint run --config ../.golangci.yaml

benchmark: ## run go benchmarks
	@go test -run=^$$ -bench=. ./...

license-check: ## check for invalid licenses
	# depends on : https://github.com/elastic/go-licence-detector
	@go list -m -mod=readonly  -json all  | go-licence-detector -includeIndirect -validate -rules allowedLicenses.json -overrides licenceOverrides.json


COVERAGE_THRESHOLD ?= 70
.PHONY: coverage
coverage: ## check code coverage per package (sqlitetest is the shared suite itself, not code to cover)
	@# -coverpkg credits a package with the statements any package's tests reach,
	@# so the core is covered by the suite the driver packages run. The per-package
	@# figures come from the merged profile: a block counts once, hit if any run hit it.
	@pkgs=$$(go list ./gormdb/... | grep -v /internal/sqlitetest | tr '\n' ' '); \
	[ -n "$$pkgs" ] || { echo "coverage: no packages to test"; exit 1; }; \
	profile=$$(mktemp) || exit 1; trap 'rm -f "$$profile"' EXIT; \
	out=$$(go test -covermode=atomic -coverpkg="$$(echo $$pkgs | tr ' ' ',')" -coverprofile="$$profile" $$pkgs 2>&1) || { echo "$$out"; exit 1; }; \
	awk -v threshold=$(COVERAGE_THRESHOLD) -v pkgs="$$pkgs" ' \
		FNR == 1 { next } \
		{ stmts[$$1] = $$2; if ($$3 > 0) hit[$$1] = 1 } \
		END { \
			for (block in stmts) { \
				pkg = block; sub(/:.*/, "", pkg); sub(/\/[^\/]*$$/, "", pkg); \
				total[pkg] += stmts[block]; if (hit[block]) covered[pkg] += stmts[block] \
			} \
			n = split(pkgs, list, " "); \
			for (i = 1; i <= n; i++) { \
				pkg = list[i]; \
				if (!total[pkg]) { printf "⚠️  %-70s no statements\n", pkg; continue } \
				cov = 100 * covered[pkg] / total[pkg]; \
				if (cov < threshold) { printf "❌ %-70s %.1f%% (below %s%%)\n", pkg, cov, threshold; fail = 1 } \
				else { printf "✅ %-70s %.1f%%\n", pkg, cov } \
			} \
			exit fail \
		}' "$$profile"

.PHONY: verify
verify: ## run all checks (needs Docker for the Postgres tests); runs every check and fails if any fail
	@fail=0; \
	for target in test-all license-check lint benchmark coverage; do \
		echo "==================== make $$target ===================="; \
		$(MAKE) --no-print-directory $$target || fail=1; \
	done; \
	if [ $$fail -ne 0 ]; then \
		echo "❌ verify failed (see above)"; \
		exit 1; \
	fi; \
	echo "✅ verify passed"

cover-report: ## generate a coverage report
	go test -covermode=count -coverpkg=./... -coverprofile cover.out  ./...
	go tool cover -html cover.out -o cover.html
	open cover.html

#==========================================================================================
##@ Release
#==========================================================================================

.PHONY: check-git-clean
check-git-clean: # check if git repo is clen
	@git diff --quiet

.PHONY: check-branch
check-branch:
	@current_branch=$$(git symbolic-ref --short HEAD) && \
	if [ "$$current_branch" != "main" ]; then \
		echo "Error: You are on branch '$$current_branch'. Please switch to 'main'."; \
		exit 1; \
	fi

check_env: # check for needed envs
	@[ "${version}" ] || ( echo ">> version is not set, usage: make release version=\"v1.2.3\" "; exit 1 )


tag: check_env check-branch check-git-clean verify ## create a tag and push to git
	@git diff --quiet || ( echo 'git is in dirty state' ; exit 1 )
	@[ "${version}" ] || ( echo ">> version is not set, usage: make release version=\"v1.2.3\" "; exit 1 )
	@git tag -d $(version) || true
	@git tag -a $(version) -m "Release version: $(version)"
	@git push --delete origin $(version) || true
	@git push origin $(version) || true



#==========================================================================================
#  Help
#==========================================================================================
.PHONY: help
help: # Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
