package pxe

// ubuntuTemplate is the PXE configuration template for Ubuntu
const ubuntuTemplate = `DEFAULT ubuntu-install
PROMPT 0
TIMEOUT 10
LABEL ubuntu-install
  MENU LABEL Install Ubuntu {{.OSVersion}}
  KERNEL {{.KernelPath}}
  APPEND initrd={{.InitrdPath}} auto=true priority=critical url={{.RegionalURL}}/preseed/{{.SerialNumber}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8
`

// centosTemplate is the PXE configuration template for CentOS
const centosTemplate = `DEFAULT centos-install
PROMPT 0
TIMEOUT 10
LABEL centos-install
  MENU LABEL Install CentOS {{.OSVersion}}
  KERNEL {{.KernelPath}}
  APPEND initrd={{.InitrdPath}} inst.ks={{.RegionalURL}}/kickstart/{{.SerialNumber}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8 inst.cmdline
`

// rockyTemplate is the PXE configuration template for Rocky Linux
const rockyTemplate = `DEFAULT rocky-install
PROMPT 0
TIMEOUT 10
LABEL rocky-install
  MENU LABEL Install Rocky Linux {{.OSVersion}}
  KERNEL {{.KernelPath}}
  APPEND initrd={{.InitrdPath}} inst.ks={{.RegionalURL}}/kickstart/{{.SerialNumber}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8 inst.cmdline
`

// debianTemplate is the PXE configuration template for Debian
const debianTemplate = `DEFAULT debian-install
PROMPT 0
TIMEOUT 10
LABEL debian-install
  MENU LABEL Install Debian {{.OSVersion}}
  KERNEL {{.KernelPath}}
  APPEND initrd={{.InitrdPath}} auto=true priority=critical url={{.RegionalURL}}/preseed/{{.SerialNumber}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8
`

// lpmosTemplate is the PXE configuration template for LPMOS agent boot
const lpmosTemplate = `DEFAULT lpmos-agent
PROMPT 0
TIMEOUT 10
LABEL lpmos-agent
  MENU LABEL LPMOS Agent Boot
  KERNEL {{.KernelPath}}
  APPEND initrd={{.InitrdPath}} regional_url={{.RegionalURL}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8 quiet splash
`

// multiBootTemplate is a multi-option boot menu template
const multiBootTemplate = `DEFAULT menu.c32
PROMPT 0
TIMEOUT 100
ONTIMEOUT ubuntu-install

MENU TITLE PXE Boot Menu - {{.Hostname}}

LABEL ubuntu-install
  MENU LABEL Install Ubuntu {{.OSVersion}}
  KERNEL {{.KernelPath}}
  APPEND initrd={{.InitrdPath}} auto=true priority=critical url={{.RegionalURL}}/preseed/{{.SerialNumber}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8

LABEL lpmos-agent
  MENU LABEL LPMOS Agent Boot (Hardware Detection)
  KERNEL /kernels/lpmos-vmlinuz
  APPEND initrd=/initrds/lpmos-initrd.img regional_url={{.RegionalURL}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8

LABEL local
  MENU LABEL Boot from local disk
  LOCALBOOT 0

MENU END
`

// rescueTemplate is a rescue/recovery boot template
const rescueTemplate = `DEFAULT rescue
PROMPT 0
TIMEOUT 10
LABEL rescue
  MENU LABEL Rescue Mode
  KERNEL {{.KernelPath}}
  APPEND initrd={{.InitrdPath}} rescue regional_url={{.RegionalURL}} {{.GetBootParams}} console=tty0 console=ttyS0,115200n8
`

// GetTemplateByName returns a template by name
func GetTemplateByName(name string) string {
	switch name {
	case "ubuntu":
		return ubuntuTemplate
	case "centos":
		return centosTemplate
	case "rocky", "rockylinux":
		return rockyTemplate
	case "debian":
		return debianTemplate
	case "lpmos":
		return lpmosTemplate
	case "multiboot":
		return multiBootTemplate
	case "rescue":
		return rescueTemplate
	default:
		return ""
	}
}

// TemplateList returns a list of available template names
func TemplateList() []string {
	return []string{
		"ubuntu",
		"centos",
		"rocky",
		"debian",
		"lpmos",
		"multiboot",
		"rescue",
	}
}
