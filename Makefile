.PHONY: init version

init:
	go run ./cmd/workledger init --output json

version:
	go run ./cmd/workledger version

validate:
	go run ./cmd/workledger config --output json

db:
	litecli ~/.local/share/workledger/worklogs.db

ruler:
	ruler apply --agents codex,claude,copilot
