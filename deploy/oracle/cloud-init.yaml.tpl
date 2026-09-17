#cloud-config
package_update: true
package_upgrade: false

bootcmd:
  - install -d -m 0750 -o root -g root /etc/utsusemi-broker

packages:
  - curl
  - ca-certificates
  - gnupg

write_files:
  - path: /etc/caddy/Caddyfile
    permissions: "0644"
    content: |
      ${broker_fqdn} {
        reverse_proxy 127.0.0.1:8787
      }

  - path: /etc/systemd/system/utsusemi-broker.service
    permissions: "0644"
    content: |
${broker_systemd_unit}

  - path: /etc/utsusemi-broker/env.example
    permissions: "0640"
    owner:
      name: root
      group: root
    content: |
      # Copy to /etc/utsusemi-broker/env after apply. Do not bake secrets into cloud-init.
      GITHUB_APP_ID=
      UTSUSEMI_GITHUB_APP_PRIVATE_KEY_FILE=/etc/utsusemi-broker/app.pem
      UTSUSEMI_CREDENTIAL_SIGNING_KEY_FILE=/etc/utsusemi-broker/signing.pem
      JWT_ISSUER=utsusemi-broker
      JWT_VERSION=1

  - path: /usr/local/sbin/utsusemi-open-broker-ports.sh
    permissions: "0755"
    content: |
${open_broker_ports_script}

runcmd:
  - /usr/local/sbin/utsusemi-open-broker-ports.sh
  - bash -euxc 'curl -fsSL "${utsusemi_binary_url}" -o /tmp/utsusemi.tar.gz'
  - bash -euxc 'tar -xzf /tmp/utsusemi.tar.gz -C /usr/local/bin utsusemi && chmod 0755 /usr/local/bin/utsusemi && rm -f /tmp/utsusemi.tar.gz'
  - bash -euxc 'install -d -m 0755 /usr/share/keyrings && curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/gpg.key" | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg && curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt" | tee /etc/apt/sources.list.d/caddy-stable.list && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y -o Dpkg::Options::="--force-confold" caddy && caddy validate --config /etc/caddy/Caddyfile'
  - systemctl daemon-reload
  - systemctl enable caddy
  - systemctl restart caddy
