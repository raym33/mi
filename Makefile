.PHONY: build test run-coordinator run-node smoke

build:
	go build -o bin/coordinator ./coordinator/cmd/coordinator
	go build -o bin/node-agent ./node-agent/cmd/node-agent

test:
	go test ./...

run-coordinator:
	go run ./coordinator/cmd/coordinator -config configs/coordinator.yaml

run-node:
	go run ./node-agent/cmd/node-agent -config configs/node-agent.yaml

smoke:
	bash scripts/smoke.sh
