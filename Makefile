.PHONY: run build clean

ADDR ?= :4433
FRONTEND ?= frontend
API_KEY ?=

run:
	go run cmd/api/main.go --addr $(ADDR) --frontend $(FRONTEND) $(if $(API_KEY),--api-key $(API_KEY),)
	@echo ""
	@echo "Frontend: http://localhost$(ADDR)"

build:
	go build -o raytester-api cmd/api/main.go
	go build -o raytest cli/main.go

clean:
	rm -f raytester-api raytest
