```
gitops-repo/
├── base/                          # Common manifests
│   ├── deployment.yaml
│   ├── service.yaml
│   └── kustomization.yaml
├── overlays/
│   ├── stg/                       # staging cluster config
│   │   ├── kustomization.yaml
│   │   └── patch-deployment.yaml   # e.g., image: myapp:1.2.4, replicas=2
│   ├── perf/                      # performance cluster config
│   │   ├── kustomization.yaml
│   │   └── patch-deployment.yaml   # replicas=5, more CPU
│   └── prod/                      # production cluster config
│       ├── kustomization.yaml
│       └── patch-deployment.yaml   # image: myapp:1.2.3 (until promoted)
└── applicationset.yaml      
```

```
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: myapp-hybrid
spec:
  generators:
    - list:
        elements:
          - name: stg
            cluster: https://stg-cluster.example.com
            branch: develop
          - name: perf
            cluster: https://perf-cluster.example.com
            branch: release
          - name: prod
            cluster: https://prod-cluster.example.com
            branch: main
  template:
    metadata:
      name: 'myapp-{{name}}'
    spec:
      project: default
      source:
        repoURL: https://github.com/org/gitops-repo
        targetRevision: '{{branch}}'
        path: overlays/{{name}}
      destination:
        server: '{{cluster}}'
        namespace: myapp

```
```
gitops-repo/
├── orders/
│   ├── base/
│   ├── overlays/
│   │   ├── stg/
│   │   ├── perf/
│   │   └── prod/
│   └── applicationset.yaml
├── payments/
│   ├── base/
│   ├── overlays/
│   │   ├── stg/
│   │   ├── perf/
│   │   └── prod/
│   └── applicationset.yaml
└── inventory/
    ├── base/
    ├── overlays/
    │   ├── stg/
    │   ├── perf/
    │   └── prod/
    └── applicationset.yaml
```
