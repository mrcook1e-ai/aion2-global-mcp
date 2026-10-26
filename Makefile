.PHONY: build build-win test vet deploy-win

build:
	go build -o bin/aion2d ./cmd/aion2d

build-win:
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/aion2d.exe ./cmd/aion2d

vet:
	go vet ./...

test:
	go test -race ./...

deploy-win: build-win
	ssh mrcook1e@192.168.1.12 "powershell -NoProfile -Command \"New-Item -ItemType Directory -Force -Path ~\aion2-mcp | Out-Null; Get-Process aion2d -ErrorAction SilentlyContinue | Stop-Process -Force; exit 0\""
	scp bin/aion2d.exe mrcook1e@192.168.1.12:C:/Users/mrcook1e/aion2-mcp/aion2d.exe
