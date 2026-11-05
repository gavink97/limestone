.PHONY: update
update:
	go get -u ./...

.PHONY: start
start:
	docker compose up --build -d

.PHONY: verify
verify:
	docker compose build && \
	docker compose run --rm caddy caddy validate --config /etc/caddy/Caddyfile

.PHONY: test
test:
	go test -race -v -timeout 30s ./...

.PHONY: test-reconciler
test-reconciler:
	go run testing/main.go

.PHONY: test-load
test-load:
	hey -z 30s http://localhost:8080/
