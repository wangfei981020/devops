#!/bin/bash
set -e

echo "=== SSO Service Deployment ==="

# 配置
HARBOR_REGISTRY="harbor.leiyanhui.com"
PROJECT="opsplatform"
VERSION="v1"

# 创建命名空间
echo "Creating namespace sso..."
kubectl create namespace sso --dry-run=client -o yaml | kubectl apply -f -

# 构建后端镜像
echo "Building sso-backend..."
cd sso-backend
docker build -t ${HARBOR_REGISTRY}/${PROJECT}/sso-backend:${VERSION} .

# 推送后端镜像
echo "Pushing sso-backend..."
docker push ${HARBOR_REGISTRY}/${PROJECT}/sso-backend:${VERSION}

# 构建前端镜像
echo "Building sso-frontend..."
cd ../sso-frontend
docker build -t ${HARBOR_REGISTRY}/${PROJECT}/sso-frontend:${VERSION} .

# 推送前端镜像
echo "Pushing sso-frontend..."
docker push ${HARBOR_REGISTRY}/${PROJECT}/sso-frontend:${VERSION}

# 部署到 K8s
echo "Deploying to Kubernetes..."
cd ../sso-backend
kubectl apply -f k8s-deploy.yaml

# 等待部署完成
echo "Waiting for deployment..."
kubectl rollout status deployment/sso-backend -n sso --timeout=120s
kubectl rollout status deployment/sso-frontend -n sso --timeout=120s

# 显示状态
echo ""
echo "=== Deployment Complete ==="
kubectl get pods -n sso
kubectl get svc -n sso
kubectl get ingress -n sso

echo ""
echo "SSO Service URL: https://sso.leiyanhui.com"
echo "OIDC Discovery: https://sso.leiyanhui.com/.well-known/openid-configuration"
echo ""
echo "OpsPlplatform SSO 配置:"
echo "  Client ID:     opsplatform"
echo "  Client Secret: opsplatform-secret"
echo "  Issuer URL:    https://sso.leiyanhui.com"
