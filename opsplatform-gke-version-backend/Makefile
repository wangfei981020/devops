VERSION ?= v1
IMAGE_LOCAL = localhost:8070/opsplatform/opsplatform-gke-version-backend:$(VERSION)
IMAGE_HUB   = marks26/opsplatform-gke-version-backend:$(VERSION)

.PHONY: build push push-hub trivy apply

build:
	docker build -t $(IMAGE_LOCAL) .

trivy:
	trivy image --severity CRITICAL,HIGH,MEDIUM,LOW --exit-code 1 $(IMAGE_LOCAL)

push: trivy
	docker push $(IMAGE_LOCAL)

push-hub: trivy
	docker tag $(IMAGE_LOCAL) $(IMAGE_HUB)
	docker push $(IMAGE_HUB)

apply:
	kubectl apply -f k8s-deploy.yaml
