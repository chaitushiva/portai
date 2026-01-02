# values.yaml
# vcluster basic configuration with ingress exposure

# --- Storage ---
storage:
  persistence: false  # for testing, set to true in prod
  size: 10Gi

# --- TLS / API ---
syncer:
  extraArgs:
    - --tls-san=team-a.mycompany.internal  # TLS SAN for ingress or external DNS

# --- RBAC ---
rbac:
  clusterRole:
    create: true

# --- Sync ---
sync:
  nodes:
    enabled: true
  services:
    enabled: true
  ingresses:
    enabled: true

# --- Security ---
securityContext:
  runAsUser: 1000
  runAsGroup: 1000

# --- Ingress ---
ingress:
  enabled: true
  # Replace with the host you will access
  host: team-a.mycompany.internal
  # Optional: ingress class if you use nginx, traefik, etc.
  className: nginx
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod  # if using cert-manager for TLS
  tls:
    - hosts:
        - team-a.mycompany.internal
      secretName: team-a-vcluster-tls
helm install team-a vcluster \
  --repo https://charts.loft.sh \
  --namespace team-a-vcluster \
  --create-namespace \
  -f values.yaml
