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

## 🏗 Architecture

Terrakubed compiles into a single executable that dynamically activates internal component routers based on the `SERVICE_TYPE` environment variable. 

This means you can continue running it as isolated microservices in Kubernetes, or run everything in a single lightweight container.

- `SERVICE_TYPE=registry`: Starts only the `/terraform/modules/v1/` and `/terraform/providers/v1/` REST endpoints.
- `SERVICE_TYPE=executor`: Starts the job polling cycle or the `/api/v1/terraform-rs` webhook listener to run infrastructure pipelines.
- `SERVICE_TYPE=all`: (Default) Starts all systems concurrently for a fully embedded local development experience.

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

A unified multi-stage Dockerfile is provided to package all necessary tools (like Git, Bash, OpenSSH) and the Go binary into a tiny Alpine image.

```bash
docker build -t terrakubed:latest .
docker run -e SERVICE_TYPE=all -p 8075:8075 -p 8090:8090 terrakubed:latest
```

## 📖 Configuration

Terrakubed accepts a wide variety of environment variables to configure its storage backends, database connections, and execution paths.

Some common variables include:

* `SERVICE_TYPE`: (registry | executor | all)
* `STORAGE_TYPE`: (AWS | AZURE | GCP | LOCAL)
* `PORT`: Internal port binding
* `TERRAKUBE_API_URL`: Path to the core Terrakube Spring Boot API
* `AWS_REGION` / `AWS_BUCKET_NAME`: Core AWS Cloud Storage settings (similar for GCP/Azure)

## 🤝 Contributing

We welcome contributions! As we unify more of the Terrakube ecosystem into Go, there are many opportunities to help optimize our Terraform execution engine, expand storage drivers, and enhance API reliability. Please read our [Contribution Guide](../CONTRIBUTING.md) for more details.

## 📄 License

This project is licensed under the Apache License 2.0.
