TAG=icanhazlb-api:latest
FULLTAG=$(TAG)
DOCKERFILE=Dockerfile
all: build

build:
	docker build -t $(FULLTAG) -f $(DOCKERFILE) .

buildx:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(FULLTAG) -f $(DOCKERFILE) .

buildx-push:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(FULLTAG) --push -f $(DOCKERFILE) .

push: build
	docker push $(FULLTAG)
