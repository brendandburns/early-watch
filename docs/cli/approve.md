# watchctl approve

The `approve` command contains subcommands for pre-approving Kubernetes resource operations.  Each subcommand signs a resource path or patch with a local RSA private key and writes the resulting base64-encoded signature as an annotation that the EarlyWatch admission webhook later verifies.

See [rule-types/approval-check.md](../rule-types/approval-check.md) for a full end-to-end walkthrough.

---

## Subcommands

| Subcommand | Description |
|------------|-------------|
| [`watchctl approve delete`](#watchctl-approve-delete) | Sign a resource's canonical path to pre-approve a deletion. |
| [`watchctl approve change`](#watchctl-approve-change) | Sign the merge patch for a resource modification and output the annotated resource JSON. |

---

## watchctl approve delete

Signs a Kubernetes resource's canonical path with an RSA private key and writes the resulting signature as an annotation on the resource.  The annotation is verified by the `ApprovalCheck` rule when a `DELETE` request arrives.

### Usage

```bash
watchctl approve delete \
  --private-key /path/to/private-key.pem \
  --group "" \
  --version v1 \
  --resource configmaps \
  --namespace default \
  --name my-config
```

### Required Flags

| Flag | Description |
|------|-------------|
| `--private-key` | Path to a PEM-encoded RSA private key (PKCS#1 or PKCS#8 format). |
| `--resource` | Plural resource name, e.g. `configmaps`, `deployments`. |
| `--name` | Name of the resource to approve. |

### Optional Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `""` | API group (empty string for core resources such as ConfigMaps and Secrets). |
| `--version` | `v1` | API version. |
| `--namespace` | `""` | Namespace of the resource. Leave empty for cluster-scoped resources. |
| `--annotation-key` | `earlywatch.io/approved` | Annotation key to write the delete-approval signature to. Must match `approvalCheck.annotationKey` in the `ChangeValidator`. |
| `--kubeconfig` | `""` | Path to the kubeconfig file. Defaults to in-cluster config when empty. |

---

## watchctl approve change

Fetches the current resource state from the cluster, computes the JSON merge patch between it and the desired new state (provided as a YAML or JSON file), signs the patch with an RSA private key, and writes the approval signature as a change-approval annotation into the new resource object, which is then printed as JSON to stdout.

The output can be applied directly with `kubectl apply -f -`, which submits the annotated object to the cluster.  The EarlyWatch admission webhook verifies the annotation when the `UPDATE` request arrives.

### Usage

```bash
watchctl approve change \
  --private-key /path/to/private-key.pem \
  --group "" \
  --version v1 \
  --resource configmaps \
  --namespace default \
  --name my-config \
  --file new-config.yaml | kubectl apply -f -
```

### Required Flags

| Flag | Description |
|------|-------------|
| `--private-key` | Path to a PEM-encoded RSA private key (PKCS#1 or PKCS#8 format). |
| `--resource` | Plural resource name, e.g. `configmaps`, `deployments`. |
| `--name` | Name of the resource whose change is being approved. |
| `--file` | Path to the YAML or JSON file containing the desired new resource state. |

### Optional Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--group` | `""` | API group (empty string for core resources such as ConfigMaps and Secrets). |
| `--version` | `v1` | API version. |
| `--namespace` | `""` | Namespace of the resource. Leave empty for cluster-scoped resources. |
| `--annotation-key` | `earlywatch.io/change-approved` | Annotation key to write the change-approval signature to. Must match `approvalCheck.annotationKey` in the `ChangeValidator`. |
| `--kubeconfig` | `""` | Path to the kubeconfig file. Defaults to in-cluster config when empty. |

---

## Key Format

Both subcommands accept RSA private keys in:

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

Signatures are computed using **RSA-PSS with SHA-256**.

- `approve delete` signs the resource's canonical path string.
- `approve change` signs the JSON merge patch between the current and desired resource state.

The canonical path format is:

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
