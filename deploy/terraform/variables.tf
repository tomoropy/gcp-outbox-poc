variable "project_id" {
  type        = string
  default     = "sandbox-500107"
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
  description = "Cloud Run Worker Pool instances"
}

variable "enable_scheduler" {
  type        = bool
  default     = false
  description = "Create Cloud Scheduler jobs"
}
