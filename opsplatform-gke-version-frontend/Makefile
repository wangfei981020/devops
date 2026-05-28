VERSION ?= v2
IMAGE_LOCAL = localhost:8070/opsplatform/opsplatform-gke-version-frontend:$(VERSION)
IMAGE_HUB   = marks26/opsplatform-gke-version-frontend:$(VERSION)

.PHONY: audit build trivy push push-hub apply

audit:
	npm audit --omit=dev --audit-level=low

build: audit
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
