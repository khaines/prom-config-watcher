VERSION=0.1.0
GOOS=linux
OUTPUTFILE=prom-config-watcher
DOCKER_IMAGE=prom-config-watcher

all: build docker

docker: build
	docker build -t $(DOCKER_IMAGE):$(VERSION) .

build:
	GOOS=$(GOOS) go build -a --ldflags '-extldflags "-static"' -tags netgo -installsuffix netgo -o $(OUTPUTFILE)

