# Deployment Guide

This guide covers various deployment options for cargobay.

## Docker Compose

### Basic Setup

Create a `docker-compose.yml`:

```yaml
version: '3.8'

services:
  cargobay:
    image: cargobay:latest
    ports:
      - "8080:8080"
      - "3000:3000"
    environment:
      - CB_DATABASE_URL=postgresql://postgres:secret@postgres:5432/cargobay
      - CB_REDIS_URL=redis://redis:6379
      - CB_STORAGE_TYPE=local
      - CB_STORAGE_PATH=/var/lib/cargobay/storage
      - CB_SESSION_SECRET=your-secret-here
    volumes:
      - cargobay-storage:/var/lib/cargobay/storage
    depends_on:
      - postgres
      - redis
    restart: unless-stopped

  postgres:
    image: postgres:15
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=secret
      - POSTGRES_DB=cargobay
    volumes:
      - postgres-data:/var/lib/postgresql/data
    restart: unless-stopped

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    volumes:
      - redis-data:/data
    restart: unless-stopped

volumes:
  cargobay-storage:
  postgres-data:
  redis-data:
```

### Production Setup

```yaml
version: '3.8'

services:
  cargobay:
    image: cargobay:latest
    ports:
      - "8080:8080"
      - "3000:3000"
    environment:
      - CB_DATABASE_URL=postgresql://${DB_USER}:${DB_PASS}@postgres:5432/${DB_NAME}
      - CB_REDIS_URL=redis://redis:6379
      - CB_STORAGE_TYPE=s3
      - CB_STORAGE_S3_REGION=${AWS_REGION}
      - CB_STORAGE_S3_BUCKET=${AWS_S3_BUCKET}
      - CB_STORAGE_S3_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - CB_STORAGE_S3_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - CB_SESSION_SECRET=${SESSION_SECRET}
      - CB_ENCRYPTION_KEY=${ENCRYPTION_KEY}
      - CB_LOG_LEVEL=warn
    volumes:
      - cargobay-storage:/var/lib/cargobay/storage
    depends_on:
      - postgres
      - redis
    restart: unless-stopped
    deploy:
      replicas: 3
      resources:
        limits:
          cpus: '2'
          memory: 4G
        reservations:
          cpus: '1'
          memory: 2G

  postgres:
    image: postgres:15
    environment:
      - POSTGRES_USER=${DB_USER}
      - POSTGRES_PASSWORD=${DB_PASS}
      - POSTGRES_DB=${DB_NAME}
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./init.sql:/docker-entrypoint-initdb.d/init.sql
    restart: unless-stopped
    deploy:
      resources:
        limits:
          memory: 2G

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes --maxmemory 1gb --maxmemory-policy allkeys-lru
    volumes:
      - redis-data:/data
    restart: unless-stopped

volumes:
  cargobay-storage:
  postgres-data:
  redis-data:
```

## Kubernetes

### Manual YAML Manifests

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: cargobay
---
# secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: cargobay-secrets
  namespace: cargobay
type: Opaque
data:
  session-secret: <base64-encoded-secret>
  encryption-key: <base64-encoded-key>
  db-password: <base64-encoded-db-password>
---
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cargobay-config
  namespace: cargobay
data:
  database-url: "postgresql://cargobay:PASSWORD@postgres:5432/cargobay"
  redis-url: "redis://redis:6379"
  storage-type: "s3"
  storage-s3-region: "us-east-1"
  storage-s3-bucket: "cargobay-artifacts"
---
# postgres.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-pvc
  namespace: cargobay
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 50Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
  namespace: cargobay
spec:
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:15
          ports:
            - containerPort: 5432
          env:
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: cargobay-secrets
                  key: db-password
          volumeMounts:
            - name: postgres-storage
              mountPath: /var/lib/postgresql/data
      volumes:
        - name: postgres-storage
          persistentVolumeClaim:
            claimName: postgres-pvc
---
# redis.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: cargobay
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
        - name: redis
          image: redis:7-alpine
          ports:
            - containerPort: 6379
          command:
            - redis-server
            - --appendonly
            - "yes"
---
# cargobay.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cargobay
  namespace: cargobay
spec:
  replicas: 3
  selector:
    matchLabels:
      app: cargobay
  template:
    metadata:
      labels:
        app: cargobay
    spec:
      containers:
        - name: cargobay
          image: cargobay:latest
          ports:
            - containerPort: 8080
            - containerPort: 3000
          envFrom:
            - configMapRef:
                name: cargobay-config
            - secretRef:
                name: cargobay-secrets
          resources:
            limits:
              cpu: 2
              memory: 4Gi
            requests:
              cpu: 1
              memory: 2Gi
          volumeMounts:
            - name: cargobay-storage
              mountPath: /var/lib/cargobay/storage
      volumes:
        - name: cargobay-storage
          persistentVolumeClaim:
            claimName: cargobay-pvc
---
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: cargobay
  namespace: cargobay
spec:
  selector:
    app: cargobay
  ports:
    - name: http
      port: 80
      targetPort: 8080
    - name: ui
      port: 3000
      targetPort: 3000
  type: ClusterIP
---
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: cargobay
  namespace: cargobay
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  ingressClassName: nginx
  rules:
    - host: cargobay.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: cargobay
                port:
                  number: 80
---
# pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: cargobay-pvc
  namespace: cargobay
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Gi
```

### Helm Chart

Using the provided Helm chart:

```bash
# Install with defaults
helm install cargobay ./charts/cargobay

# Install with custom values
helm install cargobay ./charts/cargobay -f values.yaml

# Install to specific namespace
helm install cargobay ./charts/cargobay -n cargobay --create-namespace

# Upgrade
helm upgrade cargobay ./charts/cargobay -f values.yaml

# Uninstall
helm uninstall cargobay
```

### Helm Values

```yaml
# values.yaml
replicaCount: 3

image:
  repository: cargobay
  tag: latest
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 80
  uiPort: 3000

resources:
  limits:
    cpu: 2
    memory: 4Gi
  requests:
    cpu: 1
    memory: 2Gi

storage:
  type: s3
  s3:
    region: us-east-1
    bucket: cargobay-artifacts
    accessKey: YOUR_ACCESS_KEY
    secretKey: YOUR_SECRET_KEY

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: cargobay.example.com
      paths:
        - path: /
          pathType: Prefix

resources:
  limits:
    cpu: 2
    memory: 4Gi
  requests:
    cpu: 1
    memory: 2Gi

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 80

postgresql:
  enabled: true
  auth:
    username: cargobay
    password: secret
    database: cargobay
  primary:
    resources:
      limits:
        cpu: 1
        memory: 2Gi

redis:
  enabled: true
  auth:
    password: secret
  master:
    resources:
      limits:
        cpu: 500m
        memory: 512Mi
```

## AWS ECS

### Task Definition

```json
{
  "family": "cargobay",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "2048",
  "memory": "4096",
  "executionRoleArn": "arn:aws:iam::ACCOUNT:role/ecsExecutionRole",
  "taskRoleArn": "arn:aws:iam::ACCOUNT:role/ecsTaskRole",
  "containerDefinitions": [
    {
      "name": "cargobay",
      "image": "cargobay:latest",
      "portMappings": [
        {"containerPort": 8080, "hostPort": 8080, "protocol": "tcp"},
        {"containerPort": 3000, "hostPort": 3000, "protocol": "tcp"}
      ],
      "environment": [
        {"name": "CB_DATABASE_URL", "value": "postgresql://USER:PASS@RDS_ENDPOINT:5432/cargobay"},
        {"name": "CB_REDIS_URL", "value": "redis://CLUSTER_ENDPOINT:6379"},
        {"name": "CB_STORAGE_TYPE", "value": "s3"},
        {"name": "CB_STORAGE_S3_REGION", "value": "us-east-1"},
        {"name": "CB_STORAGE_S3_BUCKET", "value": "cargobay-artifacts"},
        {"name": "CB_STORAGE_S3_ACCESS_KEY_ID", "valueFrom": "arn:aws:secretsmanager:us-east-1:ACCOUNT:secret:access-key-XYZ:ACCESS_KEY::"},
        {"name": "CB_STORAGE_S3_SECRET_ACCESS_KEY", "valueFrom": "arn:aws:secretsmanager:us-east-1:ACCOUNT:secret:secret-key-XYZ:SECRET_KEY::"}
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/cargobay",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "ecs"
        }
      },
      "healthCheck": {
        "command": ["CMD", "curl", "-f", "http://localhost:8080/health"],
        "interval": 30,
        "timeout": 5,
        "retries": 3
      }
    }
  ]
}
```

## Azure Container Instances

```yaml
#aci.yaml
apiVersion: '2023-05-01'
location: eastus
name: cargobay
properties:
  containers:
    - name: cargobay
      properties:
        image: cargobay:latest
        resources:
          requests:
            cpu: 2
            memoryInGB: 4
        ports:
          - port: 8080
            protocol: TCP
          - port: 3000
            protocol: TCP
        environmentVariables:
          - name: CB_DATABASE_URL
            value: postgresql://USER:PASS@HOST:5432/cargobay
          - name: CB_REDIS_URL
            value: redis://HOST:6379
          - name: CB_STORAGE_TYPE
            value: azure
          - name: CB_STORAGE_ACCOUNT_NAME
            value: mystorageaccount
          - name: CB_STORAGE_ACCOUNT_KEY
            value: mykey
  osType: Linux
  restartPolicy: Always
```

## Google Cloud Run

```yaml
# cloudrun.yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: cargobay
  namespace: cargobay
spec:
  template:
    spec:
      containers:
        - image: gcr.io/PROJECT/cargobay:latest
          ports:
            - containerPort: 8080
          env:
            - name: CB_DATABASE_URL
              value: postgresql://USER:PASS@HOST:5432/cargobay
            - name: CB_REDIS_URL
              value: redis://HOST:6379
            - name: CB_STORAGE_TYPE
              value: gcs
            - name: CB_STORAGE_GCS_BUCKET
              value: cargobay-artifacts
          resources:
            limits:
              cpu: 2
              memory: 4Gi
            requests:
              cpu: 1
              memory: 2Gi
```

## Scaling

### Horizontal Scaling

```yaml
# Kubernetes HPA
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: cargobay-hpa
  namespace: cargobay
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: cargobay
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 80
```

### Vertical Scaling

Adjust resource limits based on load:

```yaml
resources:
  limits:
    cpu: 4
    memory: 8Gi
  requests:
    cpu: 2
    memory: 4Gi
```

## Monitoring

### Prometheus Metrics

```yaml
# Add to your service
metricsPath: /metrics
port: 8080

# Kubernetes ServiceMonitor
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: cargobay-sm
  namespace: cargobay
spec:
  selector:
    matchLabels:
      app: cargobay
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
```

### Health Checks

```bash
# HTTP health check
curl http://localhost:8080/health

# PostgreSQL connection check
curl http://localhost:8080/health/postgres

# Redis connection check
curl http://localhost:8080/health/redis

# Storage check
curl http://localhost:8080/health/storage
```
