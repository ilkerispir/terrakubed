# Terrakubed

**The unified high-performance Go backend for Terrakube.**

> Currently housing the Registry and Executor services, charting the path towards a single-binary IaC platform.

Terrakubed is the consolidated Go microservices ecosystem for the Terrakube Infrastructure as Code (IaC) platform. By combining multiple core domain models (like Workspaces, Execution, and Registry storage) into a unified codebase, it significantly reduces deployment overhead, speeds up local development, and lays the foundation for a seamless, single-binary architecture.

---

## 🚀 Features

- **Consolidated Architecture**: Run multiple Terrakube components from a single, lightweight Go binary.
- **Provider & Module Registry (v1)**: Fully compliant Terraform/OpenTofu registry protocol for managing your private modules and overriding public providers.
- **Job Executor**: A dynamic job runner powered by `terraform-exec` that handles automated `plan`, `apply`, and `destroy` operations synchronously or through Kubernetes Jobs.
- **Dynamic Versioning**: Unlike older Java counterparts, Terrakubed uses `go-version` and `hc-install` to dynamically download and execute the exact version of Terraform/OpenTofu your workspace requires.
- **Cloud Native Storage**: Built-in native SDKs (AWS S3, Azure Blob, Google Cloud Storage) to persist your Terraform State, Modules, and execution logs securely.
- **Full Plan/Apply/Destroy Lifecycle**: Supports all four job types — `terraformPlan`, `terraformPlanDestroy`, `terraformApply`, `terraformDestroy` — with correct approval-gate handling.
- **Built-in Slack Notifications**: Zero-YAML Slack alerts for plan pending approval, apply/destroy success, and failure — driven purely by workspace environment variables.
- **Resilient State Management**: Robust S3 state download with multi-path fallback and 0-byte file protection, fully compatible with Terraform Cloud migration paths.

## 🏗 Architecture

Terrakubed compiles into a single executable that dynamically activates internal component routers based on the `SERVICE_TYPE` environment variable.

This means you can continue running it as isolated microservices in Kubernetes, or run everything in a single lightweight container.

- `SERVICE_TYPE=executor`: Starts the job polling cycle or the `/api/v1/terraform-rs` webhook listener to run infrastructure pipelines. **This is the Dockerfile default.**
- `SERVICE_TYPE=registry`: Starts only the `/terraform/modules/v1/` and `/terraform/providers/v1/` REST endpoints.
- `SERVICE_TYPE=all`: Starts all systems concurrently for a fully embedded local development experience.

## ⚙️ Getting Started

### Local Development

1. **Clone the repository:**
   ```bash
   git clone https://github.com/ilkerispir/terrakubed.git
   cd terrakubed
   ```

2. **Download Dependencies & Build:**
   ```bash
   go mod download
   go build -o terrakubed cmd/terrakubed/main.go
   ```

3. **Run the Service:**
   ```bash
   # Run all embedded services
   export SERVICE_TYPE=all
   export PORT=8075
   ./terrakubed
   ```

### Docker

A unified multi-stage Dockerfile is provided to package all necessary tools (Git, Bash, OpenSSH, `jq`) and the Go binary into a tiny Alpine image.

```bash
docker build -t terrakubed:latest .
docker run -e SERVICE_TYPE=all -p 8075:8075 -p 8090:8090 terrakubed:latest
```

## 📖 Configuration

Terrakubed accepts a wide variety of environment variables to configure its storage backends, database connections, and execution paths.

### Core Variables

| Variable | Description |
|---|---|
| `SERVICE_TYPE` | `executor` \| `registry` \| `all` |
| `STORAGE_TYPE` | `AWS` \| `AZURE` \| `GCP` \| `LOCAL` |
| `PORT` | Internal port binding |
| `TERRAKUBE_API_URL` | Path to the core Terrakube Spring Boot API |
| `TERRAKUBE_UI_URL` or `TerrakubeUiURL` | Terrakube UI base domain (e.g. `app.example.com`) — used to generate deep links in Slack notifications |
| `AWS_REGION` / `AWS_BUCKET_NAME` | Core AWS Cloud Storage settings (similar for GCP/Azure) |

### Slack Notifications

Terrakubed has built-in Slack notifications that require **no changes to your workflow YAML**. Configure them via workspace environment variables:

| Variable | Required | Description |
|---|---|---|
| `SLACK_WEBHOOK_URL` | Yes | Incoming webhook URL for your Slack channel |
| `ENABLE_SLACK_NOTIFICATIONS` | No | Set to `true` to enable lifecycle notifications (pending, success) |

**Notification behaviour:**

| Event | Trigger | Condition |
|---|---|---|
| ⏳ Plan Ready — Awaiting Approval | `ENABLE_SLACK_NOTIFICATIONS=true` | Plan exits with code 2 (changes detected) |
| 💤 No Changes Detected | `ENABLE_SLACK_NOTIFICATIONS=true` | Plan exits with code 0 |
| ✅ Approved — Applying / Destroying | `ENABLE_SLACK_NOTIFICATIONS=true` | Apply or destroy begins after approval |
| 🚀 Apply / Destroy Completed | `ENABLE_SLACK_NOTIFICATIONS=true` | Apply or destroy finishes successfully |
| 🔴 Failure | `SLACK_WEBHOOK_URL` only | **Any** step failure, regardless of `ENABLE_SLACK_NOTIFICATIONS` |

> **Tip:** Set `SLACK_WEBHOOK_URL` globally so failures are always reported, and set `ENABLE_SLACK_NOTIFICATIONS=true` per-workspace (or globally) to get the full lifecycle.

Each notification includes the workspace name (as a deep link if `TERRAKUBE_UI_URL` is set), the repository source, branch, and Terraform/OpenTofu version.

## 📋 Workflow Templates

### Plan Only

```yaml
flow:
  - type: "terraformPlan"
    name: "Plan"
    step: 100
```

### Plan + Apply with Approval Gate

```yaml
flow:
  - type: "terraformPlan"
    name: "Plan"
    step: 100
  - type: "terraformApply"
    name: "Apply"
    step: 200
    approval: true
```

### Destroy with Approval Gate

```yaml
flow:
  - type: "terraformPlanDestroy"
    name: "Plan Destroy"
    step: 100
  - type: "terraformDestroy"
    name: "Destroy"
    step: 200
    approval: true
```

## 📦 Changelog

### v0.0.51 — Direct cloud storage backend (state persistence fix)
- **Root cause fixed**: The executor was using a local backend + manual state upload, which could silently lose state if the upload failed. Plan would then show all resources as "to add" on every run.
- `generateBackendOverride` now writes a real S3 / AzureRM / GCS backend block into `terrakube_override.tf`, matching the Java executor's approach exactly. Terraform reads and writes state directly to cloud storage — no manual download or upload required.
- `terraform init` now passes `-reconfigure` so it adopts the new backend without prompting for state migration in non-interactive mode.
- Manual `terraform.tfstate` download/upload removed from `ProcessJob` and `uploadStateAndOutput`.
- State history JSON now uses a UUID filename (`{UUID}.json`) and the correct history URL format: `{api_url}/tfstate/v1/organization/{orgId}/workspace/{wsId}/state/{UUID}.json` — matching the Java API's `TerraformStatePathService`.
- Local storage type falls back to a local file backend (unchanged behaviour for local dev).

### v0.0.50 — Fix apply stuck in queue after approval
- `SetApprovalCompleted` sets job status to `"pending"` (was `"queue"`).
- Java API `ScheduleJob` switch: `queue` → default (no-op), `pending` → `executePendingJob()` which dispatches the apply/destroy step.

### v0.0.49 — Fix apply notExecuted after approval gate
- `approval`-type step now calls `SetApprovalCompleted` instead of `SetCompleted`. `SetCompleted` was setting job=`"completed"`, causing the Java API to mark all remaining steps as `notExecuted`.
- Plan file stored at job-level path (no step ID): `organization/{orgId}/{wsId}/job/{jobId}/plan/terraformLibrary.tfplan`. Apply step now reliably finds it regardless of its own step ID.

### v0.0.48 — Use executor config `TerrakubeUiURL` for workspace links
- Slack `slackSend()` now reads `TerrakubeUiURL` from the executor's deployment config first (`TerrakubeUiURL` / `TERRAKUBE_UI_URL` env on the executor Deployment/Job), then falls back to the workspace-level `TERRAKUBE_UI_URL` env var.
- No need to set `TERRAKUBE_UI_URL` as a global org env var — if the executor deployment already has it, Slack links work automatically.

### v0.0.47 — Full Slack notification feature
- New file `internal/executor/core/executor_notify.go` with a complete, self-contained Slack notification service.
- Sends Slack block-kit messages with colour coding for plan pending, plan no-changes, approved/starting, success, and failure.
- Controlled by two workspace env vars: `SLACK_WEBHOOK_URL` and `ENABLE_SLACK_NOTIFICATIONS`.
- Failure notifications fire on `SLACK_WEBHOOK_URL` alone (no `ENABLE_SLACK_NOTIFICATIONS` guard) so failures are always reported if a webhook is set.

### v0.0.46 — Auto Slack failure notification
- Executor automatically POSTs to `SLACK_WEBHOOK_URL` when any terraform step fails.
- No YAML `onFailure` blocks needed (Java API 2.30.1 does not support `onFailure: true` in `Command`).

### v0.0.45 — Resilient state download
- `downloadState()` now tries three S3 paths in order:
  1. `tfstate/{orgId}/{wsId}/terraform.tfstate` (primary — Java API / TFC migration path)
  2. `tfstate/{orgId}/{wsId}/state/state.raw.json` (raw state fallback)
  3. `organization/{orgId}/workspace/{wsId}/state/terraform.tfstate` (legacy Go executor path)
- Files that download as 0 bytes are skipped and the next candidate is tried.
- `uploadStateAndOutput()` never writes a 0-byte `terraform.tfstate` back to S3, preventing good state from being overwritten by an empty file.

### v0.0.44 — S3 state path aligned with Java API
- State is now uploaded to `tfstate/{orgId}/{wsId}/terraform.tfstate`, matching the path used by the Terrakube Java API and the TFC migration protocol.
- Workspaces migrated from Terraform Cloud will no longer show all resources as "to add" on the first plan.

### v0.0.43 — `jq` and `terraform` available in scripts
- `jq` added to the Docker image (`apk add jq`).
- Terraform binary directory prepended to `PATH` in `EnvironmentVariables` before scripts run, so `after`/`before` scripts can call `terraform` and `jq` directly without full paths.

### v0.0.42 — `terraformPlanDestroy` support
- The executor now handles the `terraformPlanDestroy` job type, enabling the full destroy-with-approval flow: `terraformPlanDestroy` → approval gate → `terraformApply` (which applies the saved destroy plan).

## 🤝 Contributing

We welcome contributions! As we unify more of the Terrakube ecosystem into Go, there are many opportunities to help optimize our Terraform execution engine, expand storage drivers, and enhance API reliability. Please read our [Contribution Guide](../CONTRIBUTING.md) for more details.

## 📄 License

This project is licensed under the Apache License 2.0.
