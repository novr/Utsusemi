#cloud-config
package_update: true
package_upgrade: true

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
      [Unit]
      Description=utsusemi GitHub App broker
      After=network-online.target
      Wants=network-online.target
      ConditionPathExists=/etc/utsusemi-broker/env
      ConditionPathExists=/etc/utsusemi-broker/app.pem
      ConditionPathExists=/etc/utsusemi-broker/signing.pem

      [Service]
      Type=simple
      EnvironmentFile=-/etc/utsusemi-broker/env
      ExecStart=/usr/local/bin/utsusemi broker
      Restart=on-failure
      RestartSec=5
      StartLimitIntervalSec=300
      StartLimitBurst=5

      [Install]
      WantedBy=multi-user.target

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

runcmd:
  - bash -euxc 'curl -fsSL "${utsusemi_binary_url}" | tar -xz -C /usr/local/bin utsusemi && chmod 0755 /usr/local/bin/utsusemi'
  - bash -euxc 'install -d -m 0755 /usr/share/keyrings && curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/gpg.key" | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg && curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt" | tee /etc/apt/sources.list.d/caddy-stable.list && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y -o Dpkg::Options::="--force-confold" caddy && caddy validate --config /etc/caddy/Caddyfile'
  - systemctl daemon-reload
  - systemctl enable caddy
  - systemctl restart caddy
