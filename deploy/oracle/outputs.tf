output "instance_id" {
  description = "Compute instance OCID."
  value       = oci_core_instance.broker.id
}

output "private_ip" {
  description = "Instance private IPv4."
  value       = data.oci_core_private_ips.broker.private_ips[0].ip_address
}

output "reserved_public_ip" {
  description = "Reserved public IPv4 assigned to the broker VNIC (use for DNS A record and GitHub org IP allow list)."
  value       = oci_core_public_ip.broker_reserved.ip_address
}

output "broker_url" {
  description = "HTTPS broker URL for utsusemi configure app --broker."
  value       = "https://${var.broker_fqdn}"
}

output "ssh_user" {
  description = "SSH login user for the instance."
  value       = var.ssh_user
}
