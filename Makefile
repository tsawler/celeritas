## test: runs all tests
test:
	@go test -v ./...

## cover: opens coverage in browser
cover:
	@go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

## coverage: displays test coverage
coverage:
	@go test -cover ./...

## build_cli: builds the command line tool celeritas and copies it to myapp
build_cli:
	@go build -o ../myapp/celeritas ./cmd/cli

## build: builds the command line tool dist directory
build:
	@go build -o ./dist/celeritas ./cmd/cli
# windows users should delete the line above this one, and use the line below instead (uncommented)
#@go build -o dist/celeritas.exe ./cmd/cli
