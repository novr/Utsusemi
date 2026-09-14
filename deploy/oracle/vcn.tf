resource "oci_core_vcn" "broker" {
  count = var.create_vcn ? 1 : 0

  compartment_id = var.compartment_id
  cidr_blocks    = [var.vcn_cidr]
  display_name   = "${var.instance_display_name}-vcn"
  dns_label      = "utsusemi"
}

resource "oci_core_internet_gateway" "broker" {
  count = var.create_vcn ? 1 : 0

  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.broker[0].id
  enabled        = true
  display_name   = "${var.instance_display_name}-igw"
}

resource "oci_core_default_route_table" "broker" {
  count = var.create_vcn ? 1 : 0

  manage_default_resource_id = oci_core_vcn.broker[0].default_route_table_id

  route_rules {
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_internet_gateway.broker[0].id
  }
}

resource "oci_core_subnet" "broker" {
  count = var.create_vcn ? 1 : 0

  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.broker[0].id
  cidr_block                 = var.subnet_cidr
  display_name               = "${var.instance_display_name}-subnet"
  dns_label                  = "broker"
  prohibit_public_ip_on_vnic = false
  route_table_id             = oci_core_vcn.broker[0].default_route_table_id
}

data "oci_core_subnet" "existing" {
  count     = var.create_vcn ? 0 : 1
  subnet_id = var.subnet_id
}
