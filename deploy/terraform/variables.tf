variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "region" {
  type        = string
  default     = "asia-northeast1"
  description = "GCP region"
}

variable "image" {
  type        = string
  description = "Container image URL"
}

variable "spanner_processing_units" {
  type        = number
  default     = 100
  description = "Spanner processing units"
}

variable "worker_instance_count" {
  type        = number
  default     = 1
  description = "Cloud Run Worker Pool manual instances"
}

variable "worker_batch_size" {
  type        = number
  default     = 10
  description = "Outbox jobs claimed per worker tick"
}

variable "worker_process_delay" {
  type        = string
  default     = "0s"
  description = "Artificial per-job processing delay for load/scaling experiments"
}

variable "enable_scheduler" {
  type        = bool
  default     = false
  description = "Create Cloud Scheduler jobs"
}
