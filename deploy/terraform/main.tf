locals {
  name             = "gcp-outbox-poc"
  spanner_database = "gcp-outbox-poc"
}

resource "google_project_service" "services" {
  for_each = toset([
    "artifactregistry.googleapis.com",
    "run.googleapis.com",
    "spanner.googleapis.com",
    "cloudscheduler.googleapis.com",
    "monitoring.googleapis.com",
    "logging.googleapis.com",
  ])

  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "repo" {
  location      = var.region
  repository_id = local.name
  format        = "DOCKER"

  depends_on = [google_project_service.services]
}

resource "google_spanner_instance" "main" {
  name             = local.name
  config           = "regional-${var.region}"
  display_name     = local.name
  processing_units = var.spanner_processing_units
  edition          = "STANDARD"

  labels = {
    app = local.name
  }

  depends_on = [google_project_service.services]
}

resource "google_spanner_database" "main" {
  instance            = google_spanner_instance.main.name
  name                = local.spanner_database
  deletion_protection = false

  ddl = split(";\n", trimsuffix(trimspace(file("${path.module}/../../migrations/schema.sql")), ";"))
}

resource "google_service_account" "api" {
  account_id   = "outbox-poc-api"
  display_name = "Outbox PoC API"
}

resource "google_service_account" "worker" {
  account_id   = "outbox-poc-worker"
  display_name = "Outbox PoC Worker"
}

resource "google_service_account" "job" {
  account_id   = "outbox-poc-job"
  display_name = "Outbox PoC Jobs"
}

resource "google_service_account" "simulator" {
  account_id   = "outbox-poc-simulator"
  display_name = "Outbox PoC Simulator"
}

resource "google_spanner_database_iam_member" "api" {
  instance = google_spanner_instance.main.name
  database = google_spanner_database.main.name
  role     = "roles/spanner.databaseUser"
  member   = "serviceAccount:${google_service_account.api.email}"
}

resource "google_spanner_database_iam_member" "worker" {
  instance = google_spanner_instance.main.name
  database = google_spanner_database.main.name
  role     = "roles/spanner.databaseUser"
  member   = "serviceAccount:${google_service_account.worker.email}"
}

resource "google_spanner_database_iam_member" "job" {
  instance = google_spanner_instance.main.name
  database = google_spanner_database.main.name
  role     = "roles/spanner.databaseUser"
  member   = "serviceAccount:${google_service_account.job.email}"
}

resource "google_project_iam_member" "api_metric_writer" {
  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.api.email}"
}

resource "google_project_iam_member" "worker_metric_writer" {
  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.worker.email}"
}

resource "google_project_iam_member" "job_metric_writer" {
  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.job.email}"
}

resource "google_cloud_run_v2_service" "api" {
  name                = "${local.name}-api"
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  lifecycle {
    ignore_changes = [
      scaling,
    ]
  }

  template {
    service_account = google_service_account.api.email

    scaling {
      min_instance_count = 0
      max_instance_count = 3
    }

    containers {
      image   = var.image
      command = ["/app/api"]

      env {
        name  = "GCP_PROJECT_ID"
        value = var.project_id
      }
      env {
        name  = "SPANNER_INSTANCE_ID"
        value = google_spanner_instance.main.name
      }
      env {
        name  = "SPANNER_DATABASE_ID"
        value = google_spanner_database.main.name
      }
    }
  }
}

resource "google_cloud_run_v2_service" "simulator" {
  name                = "${local.name}-simulator"
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  lifecycle {
    ignore_changes = [
      scaling,
    ]
  }

  template {
    service_account = google_service_account.simulator.email

    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      image   = var.image
      command = ["/app/simulator"]

      env {
        name  = "GCP_PROJECT_ID"
        value = var.project_id
      }
    }
  }
}

resource "google_cloud_run_v2_service_iam_member" "simulator_public_invoker" {
  project  = var.project_id
  location = google_cloud_run_v2_service.simulator.location
  name     = google_cloud_run_v2_service.simulator.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_worker_pool" "worker" {
  name                = "${local.name}-worker"
  location            = var.region
  deletion_protection = false

  lifecycle {
    ignore_changes = [
      scaling[0].scaling_mode,
    ]
  }

  template {
    service_account = google_service_account.worker.email

    containers {
      image   = var.image
      command = ["/app/worker"]

      env {
        name  = "GCP_PROJECT_ID"
        value = var.project_id
      }
      env {
        name  = "SPANNER_INSTANCE_ID"
        value = google_spanner_instance.main.name
      }
      env {
        name  = "SPANNER_DATABASE_ID"
        value = google_spanner_database.main.name
      }
      env {
        name  = "WORKER_BATCH_SIZE"
        value = tostring(var.worker_batch_size)
      }
      env {
        name  = "WORKER_PROCESS_DELAY"
        value = var.worker_process_delay
      }
    }
  }

  scaling {
    scaling_mode          = "MANUAL"
    manual_instance_count = var.worker_instance_count
  }
}

resource "google_cloud_run_v2_job" "expire_billing" {
  name                = "${local.name}-expire-billing"
  location            = var.region
  deletion_protection = false

  template {
    template {
      service_account = google_service_account.job.email

      containers {
        image   = var.image
        command = ["/app/expire_billing"]

        env {
          name  = "GCP_PROJECT_ID"
          value = var.project_id
        }
        env {
          name  = "SPANNER_INSTANCE_ID"
          value = google_spanner_instance.main.name
        }
        env {
          name  = "SPANNER_DATABASE_ID"
          value = google_spanner_database.main.name
        }
      }
    }
  }
}

resource "google_cloud_run_v2_job" "outbox_cleanup" {
  name                = "${local.name}-outbox-cleanup"
  location            = var.region
  deletion_protection = false

  template {
    template {
      service_account = google_service_account.job.email

      containers {
        image   = var.image
        command = ["/app/outbox_cleanup"]

        env {
          name  = "GCP_PROJECT_ID"
          value = var.project_id
        }
        env {
          name  = "SPANNER_INSTANCE_ID"
          value = google_spanner_instance.main.name
        }
        env {
          name  = "SPANNER_DATABASE_ID"
          value = google_spanner_database.main.name
        }
      }
    }
  }
}

resource "google_service_account" "scheduler" {
  count        = var.enable_scheduler ? 1 : 0
  account_id   = "outbox-poc-scheduler"
  display_name = "Outbox PoC Scheduler"
}

resource "google_project_iam_member" "scheduler_run_admin" {
  count   = var.enable_scheduler ? 1 : 0
  project = var.project_id
  role    = "roles/run.admin"
  member  = "serviceAccount:${google_service_account.scheduler[0].email}"
}

resource "google_project_iam_member" "scheduler_token_creator" {
  count   = var.enable_scheduler ? 1 : 0
  project = var.project_id
  role    = "roles/iam.serviceAccountTokenCreator"
  member  = "serviceAccount:${google_service_account.scheduler[0].email}"
}

resource "google_cloud_scheduler_job" "expire_billing" {
  count       = var.enable_scheduler ? 1 : 0
  name        = "${local.name}-expire-billing"
  region      = var.region
  schedule    = "*/10 * * * *"
  time_zone   = "Asia/Tokyo"
  description = "Run expire billing scanner"

  http_target {
    http_method = "POST"
    uri         = "https://${var.region}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${var.project_id}/jobs/${google_cloud_run_v2_job.expire_billing.name}:run"

    oauth_token {
      service_account_email = google_service_account.scheduler[0].email
    }
  }
}
