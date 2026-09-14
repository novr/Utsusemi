locals {
  ssh_authorized_keys = trimspace(var.ssh_public_key) == "" ? null : trimspace(var.ssh_public_key)
  utsusemi_binary_url = trimspace(var.utsusemi_binary_url) != "" ? trimspace(var.utsusemi_binary_url) : "https://github.com/novr/utsusemi/releases/download/v${var.utsusemi_version}/utsusemi_${var.utsusemi_version}_linux_arm64.tar.gz"
  user_data = templatefile("${path.module}/cloud-init.yaml.tpl", {
    broker_fqdn         = var.broker_fqdn
    utsusemi_binary_url = local.utsusemi_binary_url
  })
}

resource "oci_core_instance" "broker" {
  availability_domain = var.availability_domain
  compartment_id      = var.compartment_id
  display_name        = var.instance_display_name
  shape               = "VM.Standard.A1.Flex"

  shape_config {
    ocpus         = 1
    memory_in_gbs = 1
  }

  source_details {
    source_type = "image"
    source_id   = var.image_ocid
  }

  create_vnic_details {
    subnet_id        = var.subnet_id
    assign_public_ip = false
    nsg_ids          = [oci_core_network_security_group.broker.id]
  }

  metadata = merge(
    { user_data = base64encode(local.user_data) },
    local.ssh_authorized_keys == null ? {} : { ssh_authorized_keys = local.ssh_authorized_keys },
  )
}

data "oci_core_vnic_attachments" "broker" {
  compartment_id = var.compartment_id
  instance_id    = oci_core_instance.broker.id
}

data "oci_core_vnic" "broker" {
  vnic_id = data.oci_core_vnic_attachments.broker.vnic_attachments[0].vnic_id
}

data "oci_core_private_ips" "broker" {
  vnic_id = data.oci_core_vnic.broker.vnic_id
}

resource "oci_core_public_ip" "broker_reserved" {
  compartment_id = var.compartment_id
  display_name   = "${var.instance_display_name}-reserved-ipv4"
  lifetime       = "RESERVED"
  public_ip_id   = var.reserved_public_ip_ocid
  private_ip_id  = data.oci_core_private_ips.broker.private_ips[0].id
}
