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

variable "autoscaler_min_workers" {
  type        = number
  default     = 1
  description = "Minimum Worker Pool instances managed by the autoscaler job"
}

variable "autoscaler_max_workers" {
  type        = number
  default     = 5
  description = "Maximum Worker Pool instances managed by the autoscaler job"
}

variable "autoscaler_scale_up_backlog" {
  type        = number
  default     = 100
  description = "Ready backlog threshold to scale up to autoscaler_scale_up_workers"
}

variable "autoscaler_scale_up_workers" {
  type        = number
  default     = 3
  description = "Worker count used when ready backlog reaches autoscaler_scale_up_backlog"
}

variable "autoscaler_max_backlog" {
  type        = number
  default     = 500
  description = "Ready backlog threshold to scale up to autoscaler_max_workers"
}

variable "autoscaler_oldest_backlog_age" {
  type        = string
  default     = "5m"
  description = "Oldest ready backlog age that triggers a one-step scale-up"
}

variable "autoscaler_dry_run" {
  type        = bool
  default     = false
  description = "Log autoscaler decisions without updating the Worker Pool"
}

variable "enable_scheduler" {
  type        = bool
  default     = false
  description = "Create Cloud Scheduler jobs"
}
