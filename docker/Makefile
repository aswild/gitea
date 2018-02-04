#Makefile related to docker

DOCKER_IMAGE ?= awild/gitea
DOCKER_TAG ?= latest
DOCKER_REF := $(DOCKER_IMAGE):$(DOCKER_TAG)


.PHONY: docker
docker:
	docker run -ti --rm -v $(CURDIR):/srv/app/src/code.gitea.io/gitea -w /srv/app/src/code.gitea.io/gitea \
		-e TAGS="bindata $(filter-out bindata,$(TAGS))" webhippie/golang:edge make clean generate build
	docker build --disable-content-trust=false -t $(DOCKER_REF) .
