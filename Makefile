APP := my_media
BIN := bin
MYDOCKER := /Users/wangdante/D/mydocker

.PHONY: build dev tidy clean migrate docker-up docker-down docker-logs docker-rebuild

build:
	go build -o $(BIN)/mediaapi .

dev:
	gf run main.go

tidy:
	go mod tidy

migrate:                        ## goose 迁移(my_media 库)
	goose -dir manifest/sql/migrations postgres "host=127.0.0.1 port=5432 user=postgres password=654321 dbname=my_media sslmode=disable" up

clean:
	rm -rf $(BIN)

# ---- Docker：走 mydocker 主编排 ----
docker-up:
	cd $(MYDOCKER) && docker compose up -d --build my_media
	@echo "探活: curl -sS http://127.0.0.1:8004/api.json | head"

docker-down:
	cd $(MYDOCKER) && docker compose stop my_media

docker-logs:
	docker logs -f my_media

docker-rebuild:
	cd $(MYDOCKER) && docker compose up -d --build --force-recreate my_media
