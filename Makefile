.PHONY: demo build test run-coordinator run-node run-city-coordinator run-city-node run-city-coordinator-tls run-city-node-tls dev-certs smoke city-smoke city-enroll backup anchor-hash

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

run-city-coordinator-tls:
	go run ./coordinator/cmd/coordinator -config configs/coordinator.city.tls.example.yaml

run-city-node-tls:
	go run ./node-agent/cmd/node-agent -config configs/node-agent.city.tls.example.yaml

dev-certs:
	bash scripts/dev-certs.sh

smoke:
	bash scripts/smoke.sh

city-smoke:
	bash scripts/city-smoke.sh

city-enroll:
	bash scripts/city-enroll.sh

backup:
	bash scripts/backup-state.sh

anchor-hash:
	bash scripts/anchor-hash.sh

demo:
	bash scripts/demo.sh
