locals {
  subnet_id = var.create_vcn ? oci_core_subnet.broker[0].id : var.subnet_id
  vcn_id    = var.create_vcn ? oci_core_vcn.broker[0].id : data.oci_core_subnet.existing[0].vcn_id
}
