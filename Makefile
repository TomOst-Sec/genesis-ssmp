.PHONY: fmt vet test lint build clean bench-proxy bench-s1 bench-full bench-analyze

fmt:
	@go fmt ./... 2>&1 || true

vet:
	@out=$$(go vet ./... 2>&1); rc=$$?; if [ $$rc -ne 0 ] && echo "$$out" | grep -q "no packages to vet"; then exit 0; else echo "$$out"; exit $$rc; fi

test:
	@out=$$(go test ./... 2>&1); rc=$$?; if [ $$rc -ne 0 ] && echo "$$out" | grep -q "no packages to test"; then exit 0; else echo "$$out"; exit $$rc; fi

lint: fmt vet

build:
	@go build ./... 2>&1 || true

clean:
	@go clean ./... 2>&1 || true

bench-proxy:
	@go build -o bench/harness/proxy/proxy ./bench/harness/proxy/

bench-s1: bench-proxy
	@bash bench/harness/harness.sh s1_star_quantifier 1

bench-full: bench-proxy
	@bash bench/harness/harness.sh all 3

bench-analyze:
	@python3 bench/harness/analyze.py /tmp/bench/runs/latest/
	@python3 bench/harness/report.py /tmp/bench/runs/latest/analysis.json
