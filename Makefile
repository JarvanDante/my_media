APP := my_media
BIN := bin

.PHONY: build dev tidy clean migrate

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
