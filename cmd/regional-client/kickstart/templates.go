package kickstart

// CentOS 7 Kickstart Template
const centos7Template = `#version=RHEL7
# LPMOS Generated Kickstart for {{.SN}}
# Generated at: {{.Timestamp}}

# System authorization information
auth --enableshadow --passalgo=sha512

# Use text mode install
text

# Run the Setup Agent on first boot
firstboot --disable

# Keyboard layouts
keyboard --vckeymap=us --xlayouts='us'

# System language
lang en_US.UTF-8

# Network information
network --bootproto=static --device={{.PrimaryNIC}} --ip={{.IP}} --netmask={{.Netmask}} --gateway={{.Gateway}} --nameserver={{.DNS}} --hostname={{.Hostname}} --activate

# Root password
rootpw --iscrypted {{.RootPasswordHash}}

# System timezone
timezone Asia/Shanghai --isUtc

# Installation source
url --url={{.RepoURL}}

# System bootloader configuration
bootloader --location=mbr --boot-drive={{.BootDisk}}

# Partition clearing information
clearpart --all --drives={{.TargetDisks}} --initlabel

# Disk partitioning information
part /boot --fstype="ext4" --ondisk={{.BootDisk}} --size=1024
part swap --fstype="swap" --ondisk={{.BootDisk}} --size=16384
part / --fstype="ext4" --ondisk={{.BootDisk}} --size=1 --grow

# SELinux configuration
selinux --disabled

# Firewall configuration
firewall --disabled

# Do not configure the X Window System
skipx

# Reboot after installation
reboot

%packages --ignoremissing
@core
@base
wget
curl
vim
net-tools
openssh-server
{{range .Packages}}
{{.}}
{{end}}
%end

%post --log=/root/ks-post.log
#!/bin/bash

# Set hostname
echo "{{.Hostname}}" > /etc/hostname
hostnamectl set-hostname {{.Hostname}}

# Configure network
cat > /etc/sysconfig/network-scripts/ifcfg-{{.PrimaryNIC}} <<EOF
DEVICE={{.PrimaryNIC}}
BOOTPROTO=static
ONBOOT=yes
IPADDR={{.IP}}
NETMASK={{.Netmask}}
GATEWAY={{.Gateway}}
DNS1={{.DNS}}
EOF

# Disable firewall
systemctl disable firewalld

# Set root password
echo "root:lpmos123" | chpasswd

# Report installation complete to Regional Client
curl -X POST "{{.RegionalURL}}/api/v1/device/installComplete" \
  -H "Content-Type: application/json" \
  -d '{"sn":"{{.SN}}","status":"success","message":"OS installed successfully"}' || true

# Execute custom post script if provided
{{if .PostScript}}
echo "{{.PostScript}}" | base64 -d > /tmp/post-install.sh
chmod +x /tmp/post-install.sh
/tmp/post-install.sh
{{end}}

%end
`

// CentOS 8 / Stream Kickstart Template
const centos8Template = `#version=RHEL8
# LPMOS Generated Kickstart for {{.SN}}
# Generated at: {{.Timestamp}}

# System language
lang en_US.UTF-8

# Keyboard layout
keyboard us

# Network information
network --bootproto=static --device={{.PrimaryNIC}} --ip={{.IP}} --netmask={{.Netmask}} --gateway={{.Gateway}} --nameserver={{.DNS}} --hostname={{.Hostname}} --activate

# Root password
rootpw --iscrypted {{.RootPasswordHash}}

# System timezone
timezone Asia/Shanghai --utc

# Use text mode install
text

# Installation source
url --url={{.RepoURL}}

# System bootloader configuration
bootloader --location=mbr --boot-drive={{.BootDisk}}

# Partition clearing information
clearpart --all --drives={{.TargetDisks}} --initlabel

# Disk partitioning (UEFI compatible)
part /boot/efi --fstype="efi" --ondisk={{.BootDisk}} --size=600 --fsoptions="umask=0077,shortname=winnt"
part /boot --fstype="xfs" --ondisk={{.BootDisk}} --size=1024
part swap --fstype="swap" --ondisk={{.BootDisk}} --size=16384
part / --fstype="xfs" --ondisk={{.BootDisk}} --size=1 --grow

# SELinux configuration
selinux --disabled

# Firewall configuration
firewall --disabled

# Do not configure the X Window System
skipx

# Reboot after installation
reboot

%packages
@^minimal-environment
wget
curl
vim
net-tools
openssh-server
{{range .Packages}}
{{.}}
{{end}}
%end

%post --log=/root/ks-post.log
#!/bin/bash

# Set hostname
hostnamectl set-hostname {{.Hostname}}

# Configure network
nmcli connection modify {{.PrimaryNIC}} ipv4.addresses {{.IP}}/{{.Netmask}}
nmcli connection modify {{.PrimaryNIC}} ipv4.gateway {{.Gateway}}
nmcli connection modify {{.PrimaryNIC}} ipv4.dns {{.DNS}}
nmcli connection modify {{.PrimaryNIC}} ipv4.method manual
nmcli connection up {{.PrimaryNIC}}

# Disable firewall
systemctl disable firewalld

# Report installation complete
curl -X POST "{{.RegionalURL}}/api/v1/device/installComplete" \
  -H "Content-Type: application/json" \
  -d '{"sn":"{{.SN}}","status":"success","message":"OS installed"}' || true

{{if .PostScript}}
echo "{{.PostScript}}" | base64 -d > /tmp/post-install.sh
chmod +x /tmp/post-install.sh
/tmp/post-install.sh
{{end}}

%end
`

// Rocky Linux 8 Kickstart Template
const rocky8Template = centos8Template

// Rocky Linux 9 Kickstart Template
const rocky9Template = centos8Template

// Ubuntu 20.04 Preseed Template
const ubuntu2004Template = `# LPMOS Generated Preseed for {{.SN}}
# Generated at: {{.Timestamp}}

#### Localization
d-i debian-installer/language string en
d-i debian-installer/country string US
d-i debian-installer/locale string en_US.UTF-8
d-i keyboard-configuration/xkb-keymap select us

#### Network configuration
d-i netcfg/choose_interface select {{.PrimaryNIC}}
d-i netcfg/disable_autoconfig boolean true
d-i netcfg/get_ipaddress string {{.IP}}
d-i netcfg/get_netmask string {{.Netmask}}
d-i netcfg/get_gateway string {{.Gateway}}
d-i netcfg/get_nameservers string {{.DNS}}
d-i netcfg/confirm_static boolean true
d-i netcfg/get_hostname string {{.Hostname}}
d-i netcfg/get_domain string localdomain

#### Mirror settings
d-i mirror/country string manual
d-i mirror/http/hostname string {{.RepoURL}}
d-i mirror/http/directory string /ubuntu
d-i mirror/http/proxy string

#### Account setup
d-i passwd/root-login boolean true
d-i passwd/root-password-crypted password {{.RootPasswordHash}}
d-i passwd/user-fullname string
d-i passwd/username string
d-i passwd/user-password-crypted password !

#### Clock and time zone setup
d-i clock-setup/utc boolean true
d-i time/zone string Asia/Shanghai
d-i clock-setup/ntp boolean true

#### Partitioning
d-i partman-auto/disk string {{.BootDisk}}
d-i partman-auto/method string regular
d-i partman-auto/choose_recipe select atomic
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true

#### Package selection
tasksel tasksel/first multiselect standard
d-i pkgsel/include string openssh-server wget curl vim net-tools
d-i pkgsel/upgrade select full-upgrade
d-i pkgsel/update-policy select none

#### Boot loader installation
d-i grub-installer/only_debian boolean true
d-i grub-installer/bootdev string {{.BootDisk}}

#### Finishing up
d-i finish-install/reboot_in_progress note

#### Late command
d-i preseed/late_command string \
    in-target curl -X POST "{{.RegionalURL}}/api/v1/device/installComplete" \
    -H "Content-Type: application/json" \
    -d '{"sn":"{{.SN}}","status":"success"}' || true
`

// Ubuntu 22.04 Preseed Template (使用 autoinstall)
const ubuntu2204Template = ubuntu2004Template
