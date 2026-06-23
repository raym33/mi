.PHONY: build test run-coordinator run-node run-city-coordinator run-city-node smoke city-smoke

build:
	go build -o bin/coordinator ./coordinator/cmd/coordinator
	go build -o bin/node-agent ./node-agent/cmd/node-agent

test:
	go test ./...

run-coordinator:
	go run ./coordinator/cmd/coordinator -config configs/coordinator.yaml

run-node:
	go run ./node-agent/cmd/node-agent -config configs/node-agent.yaml

run-city-coordinator:
	go run ./coordinator/cmd/coordinator -config configs/coordinator.city.example.yaml

run-city-node:
	go run ./node-agent/cmd/node-agent -config configs/node-agent.city.example.yaml

smoke:
	bash scripts/smoke.sh

city-smoke:
	bash scripts/city-smoke.sh
