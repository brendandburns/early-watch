# Rule Type: ApprovalCheck

The `ApprovalCheck` rule type **denies the operation unless the resource carries a valid approval annotation** whose value is the base64-encoded RSA-PSS SHA-256 signature of the resource's canonical path, signed with the private key that corresponds to the configured public key.

Use `watchctl approve` to generate and apply the signature.

---

## When to Use

Use `ApprovalCheck` when you want cryptographically verifiable approval of a specific change.  This is stronger than `AnnotationCheck` because:

- The approval is tied to a specific resource path — the signature cannot be copied to another resource.
- Only the holder of the matching RSA private key can produce a valid approval.

---

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `publicKey` | string | Yes | PEM-encoded RSA public key (PKIX/SubjectPublicKeyInfo format) used to verify the approval signature. |
| `annotationKey` | string | No | Annotation key on the resource that holds the base64-encoded signature. Defaults to `earlywatch.io/approved`. |

---

## Canonical Path Format

The signature is computed over the resource's canonical path string:

```
# Namespaced resources (named API group)
<group>/<version>/namespaces/<namespace>/<resource>/<name>

# Namespaced resources (core group, group == "")
<version>/namespaces/<namespace>/<resource>/<name>

# Cluster-scoped resources (named API group)
<group>/<version>/<resource>/<name>

# Cluster-scoped resources (core group, group == "")
<version>/<resource>/<name>
```

Examples:

```
v1/namespaces/default/configmaps/my-config
apps/v1/namespaces/production/deployments/web-api
v1/namespaces/production
```

---

## Example

### 1. Generate an RSA key pair

```bash
# Generate a 4096-bit RSA private key
openssl genrsa -out private-key.pem 4096

# Extract the corresponding public key
openssl rsa -in private-key.pem -pubout -out public-key.pem
```

### 2. Create the ChangeValidator

Embed the public key in the `ApprovalCheck`:

```yaml
apiVersion: earlywatch.io/v1alpha1
kind: ChangeValidator
metadata:
  name: require-approval-for-delete
  namespace: default
spec:
  subject:
    apiGroup: ""
    resource: configmaps
    names:
      - my-critical-config
  operations:
    - DELETE
  rules:
    - name: must-be-approved
      type: ApprovalCheck
      approvalCheck:
        publicKey: |
          -----BEGIN PUBLIC KEY-----
          MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA...
          -----END PUBLIC KEY-----
      message: >
        ConfigMap "my-critical-config" cannot be deleted without a valid
        approval signature. Run: watchctl approve --resource configmaps --name my-critical-config
```

### 3. Sign the resource with watchctl

```bash
watchctl approve \
  --private-key private-key.pem \
  --group "" \
  --version v1 \
  --resource configmaps \
  --namespace default \
  --name my-critical-config
```

This patches the `earlywatch.io/approved` annotation on the ConfigMap with the base64-encoded signature.

### 4. Delete the resource

```bash
kubectl delete configmap my-critical-config
# Succeeds — the webhook verifies the signature and allows the deletion.
```

---

## watchctl approve Reference

See [cli/approve.md](../cli/approve.md) for the full flag reference and key format details.
