variable "region" {
  description = "OCI home region (Always Free resources must stay in the tenancy home region)."
  type        = string
}

variable "compartment_id" {
  description = "Compartment OCID for compute, NSG, and reserved public IP assignment."
  type        = string
}

variable "availability_domain" {
  description = "AD name (e.g. kIam:AP-TOKYO-1-AD-1)."
  type        = string
}

variable "create_vcn" {
  description = "Create a VCN, Internet Gateway, and public subnet. Set false when reusing subnet_id."
  type        = bool
  default     = true
}

variable "vcn_cidr" {
  description = "VCN CIDR when create_vcn is true."
  type        = string
  default     = "10.42.0.0/16"
}

variable "subnet_cidr" {
  description = "Public subnet CIDR when create_vcn is true."
  type        = string
  default     = "10.42.0.0/24"
}

variable "subnet_id" {
  description = "Existing public subnet OCID. Required when create_vcn is false."
  type        = string
  default     = ""

  validation {
    condition     = var.create_vcn || trimspace(var.subnet_id) != ""
    error_message = "subnet_id is required when create_vcn is false."
  }
}

variable "image_ocid" {
  description = "Always Free eligible Ubuntu 22.04 arm64 image OCID. cloud-init uses apt; Oracle Linux is unsupported."
  type        = string
}

variable "ssh_user" {
  description = "Default SSH user for the chosen image (ubuntu for Canonical images)."
  type        = string
  default     = "ubuntu"
}

variable "reserved_public_ip_ocid" {
  description = "Existing Reserved public IPv4 OCID to attach to the instance VNIC."
  type        = string
}

variable "operator_ssh_cidr" {
  description = "CIDR allowed to reach SSH (TCP 22), typically your current public IP /32."
  type        = string

  validation {
    condition     = can(cidrhost(var.operator_ssh_cidr, 0))
    error_message = "operator_ssh_cidr must be a valid IPv4 CIDR block."
  }
}

variable "broker_fqdn" {
  description = "Public broker hostname for Caddy TLS (A record must point at the reserved IPv4 before first HTTPS)."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$", var.broker_fqdn))
    error_message = "broker_fqdn must be a lowercase hostname (no scheme or path)."
  }
}

variable "utsusemi_version" {
  description = "GitHub release tag without v prefix (e.g. 0.5.0) for the linux/arm64 binary."
  type        = string

  validation {
    condition     = trimspace(var.utsusemi_binary_url) != "" || trimspace(var.utsusemi_version) != ""
    error_message = "utsusemi_version is required when utsusemi_binary_url is empty."
  }
}

variable "utsusemi_binary_url" {
  description = "Optional override URL for the linux/arm64 tarball (default: GitHub release for utsusemi_version)."
  type        = string
  default     = ""
}

variable "instance_display_name" {
  description = "Compute instance display name."
  type        = string
  default     = "utsusemi-broker"
}

variable "ssh_public_key" {
  description = "Optional SSH public key for the default opc/ubuntu user. Leave empty to skip."
  type        = string
  default     = ""
}
