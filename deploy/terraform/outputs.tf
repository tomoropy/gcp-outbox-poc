output "artifact_registry" {
  value = google_artifact_registry_repository.repo.name
}

output "api_url" {
  value = google_cloud_run_v2_service.api.uri
}

output "simulator_url" {
  value = google_cloud_run_v2_service.simulator.uri
}

output "worker_pool_name" {
  value = google_cloud_run_v2_worker_pool.worker.name
}

output "spanner_database" {
  value = google_spanner_database.main.id
}
