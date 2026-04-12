# watchctl approve

The `approve` subcommand signs a Kubernetes resource's canonical path with a local RSA private key and writes the resulting base64-encoded signature as an annotation on the resource.  The annotation is later verified by the `ApprovalCheck` rule in the EarlyWatch admission webhook.

See [rule-types/approval-check.md](../rule-types/approval-check.md) for a full end-to-end walkthrough.

---

## Usage

```bash
watchctl approve \
  --private-key /path/to/private-key.pem \
  --group "" \
  --version v1 \
  --resource configmaps \
  --namespace default \
  --name my-config
```

---

## Required Flags

| Flag | Description |
|------|-------------|
| `--private-key` | Path to a PEM-encoded RSA private key (PKCS#1 or PKCS#8 format). |
| `--resource` | Plural resource name, e.g. `configmaps`, `deployments`. |
| `--name` | Name of the resource to approve. |

---

## Optional Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `""` | API group (empty string for core resources such as ConfigMaps and Secrets). |
| `--version` | `v1` | API version. |
| `--namespace` | `""` | Namespace of the resource. Leave empty for cluster-scoped resources. |
| `--annotation-key` | `earlywatch.io/approved` | Annotation key to write the signature to. Must match `approvalCheck.annotationKey` in the `ChangeValidator`. |
| `--kubeconfig` | `""` | Path to the kubeconfig file. Defaults to in-cluster config when empty. |

---

## Key Format

`watchctl approve` accepts RSA private keys in:

- **PKCS#1** PEM format (`-----BEGIN RSA PRIVATE KEY-----`)
- **PKCS#8** PEM format (`-----BEGIN PRIVATE KEY-----`)

Generate a key pair with OpenSSL:

```bash
# Generate a 4096-bit RSA private key
openssl genrsa -out private-key.pem 4096

# Extract the corresponding public key (embed this in ApprovalCheck.publicKey)
openssl rsa -in private-key.pem -pubout -out public-key.pem
```

---

## Signature Algorithm

The signature is computed using **RSA-PSS with SHA-256** over the resource's canonical path string.  The canonical path format is:

```
# Namespaced resources
<group>/<version>/namespaces/<namespace>/<resource>/<name>

# Cluster-scoped resources
<group>/<version>/<resource>/<name>
```

For core-group resources (`group == ""`), the path starts with a `/`:

```
/v1/namespaces/default/configmaps/my-config
apps/v1/namespaces/production/deployments/web-api
```
