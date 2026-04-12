# TLS and cert-manager

The EarlyWatch admission webhook requires a valid TLS certificate so the Kubernetes API server can establish a secure connection to it.  This document describes the TLS requirements and how to provision certificates using [cert-manager](https://cert-manager.io/).

---

## Requirements

The Kubernetes API server validates the webhook server's TLS certificate against the `caBundle` field in the `ValidatingWebhookConfiguration`.  You must therefore:

1. Have a Certificate Authority (CA) whose certificate is trusted by the API server.
2. Issue a server certificate for the webhook `Service` from that CA.
3. Set the CA bundle in the `ValidatingWebhookConfiguration`.

---

## Option A — cert-manager (Recommended)

[cert-manager](https://cert-manager.io/) automates certificate issuance and renewal.

### 1. Install cert-manager

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
```

Wait for the cert-manager Pods to be ready:

```bash
kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager \
  -n cert-manager --timeout=120s
```

### 2. Create a self-signed CA Issuer

```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: earlywatch-selfsigned
  namespace: early-watch-system
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: earlywatch-ca
  namespace: early-watch-system
spec:
  isCA: true
  commonName: earlywatch-ca
  secretName: earlywatch-ca-tls
  issuerRef:
    name: earlywatch-selfsigned
    kind: Issuer
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: earlywatch-ca
  namespace: early-watch-system
spec:
  ca:
    secretName: earlywatch-ca-tls
```

### 3. Issue a server certificate

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: earlywatch-webhook
  namespace: early-watch-system
spec:
  secretName: earlywatch-webhook-tls
  dnsNames:
    - earlywatch-webhook.early-watch-system.svc
    - earlywatch-webhook.early-watch-system.svc.cluster.local
  issuerRef:
    name: earlywatch-ca
    kind: Issuer
```

### 4. Inject the CA bundle

Use the cert-manager [CA injector](https://cert-manager.io/docs/concepts/ca-injector/) to automatically populate the `caBundle` field in the `ValidatingWebhookConfiguration`:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: earlywatch-webhook
  annotations:
    cert-manager.io/inject-ca-from: early-watch-system/earlywatch-webhook
```

### 5. Mount the certificate in the webhook Deployment

```yaml
spec:
  template:
    spec:
      containers:
        - name: webhook
          volumeMounts:
            - name: tls
              mountPath: /tls
              readOnly: true
      volumes:
        - name: tls
          secret:
            secretName: earlywatch-webhook-tls
```

Pass the paths to the webhook server via flags or environment variables as required by your deployment configuration.

---

## Option B — Manual Certificate Management

If cert-manager is not available, you can provision certificates manually:

```bash
# 1. Generate a CA key and certificate
openssl req -x509 -newkey rsa:4096 -keyout ca.key -out ca.crt \
  -days 3650 -nodes -subj "/CN=earlywatch-ca"

# 2. Generate the webhook server key and CSR
openssl req -newkey rsa:4096 -keyout webhook.key -out webhook.csr \
  -nodes -subj "/CN=earlywatch-webhook.early-watch-system.svc"

# 3. Sign the webhook certificate with the CA
openssl x509 -req -in webhook.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out webhook.crt -days 365 \
  -extfile <(printf "subjectAltName=DNS:earlywatch-webhook.early-watch-system.svc,DNS:earlywatch-webhook.early-watch-system.svc.cluster.local")

# 4. Create a Kubernetes Secret with the certificate and key
kubectl create secret tls earlywatch-webhook-tls \
  --cert=webhook.crt --key=webhook.key \
  -n early-watch-system

# 5. Patch the ValidatingWebhookConfiguration with the CA bundle
CA_BUNDLE=$(base64 < ca.crt | tr -d '\n')
kubectl patch validatingwebhookconfiguration earlywatch-webhook \
  --type='json' \
  -p="[{'op':'replace','path':'/webhooks/0/clientConfig/caBundle','value':'${CA_BUNDLE}'}]"
```

Remember to rotate certificates before they expire and update the `caBundle` accordingly.
