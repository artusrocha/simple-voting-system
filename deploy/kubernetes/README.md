# Kubernetes Manifests

This directory contains Kubernetes manifests using Kustomize for the voting platform.

## Structure

```
kubernetes/
├── base/
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── api-deployment.yaml
│   ├── api-service.yaml
│   ├── projector-deployment.yaml
│   ├── projector-service.yaml
│   ├── frontend-deployment.yaml
│   ├── frontend-service.yaml
│   └── ingress.yaml
└── overlays/
    ├── dev/
    │   └── kustomization.yaml
    └── prod/
        └── kustomization.yaml
```

## Usage

### Prerequisites

- Kubernetes cluster (minikube, kind, or cloud provider)
- kubectl configured
- kustomize installed

### Development

Apply the development configuration:

```bash
kubectl apply -k overlays/dev
```

### Production

Apply the production configuration:

```bash
kubectl apply -k overlays/prod
```

### View rendered manifests

To see the final manifests without applying:

```bash
kubectl kustomize overlays/dev
kubectl kustomize overlays/prod
```

### Customizing

To add your own overlay, create a new directory under `overlays/` and add a `kustomization.yaml` file.

## Services

| Service | Port | Description |
|---------|------|-------------|
| frontend | 80 | Frontend UI |
| api | 80 (8080 internal) | REST API |
| projector | 80 (8081 internal) | Vote projector |
