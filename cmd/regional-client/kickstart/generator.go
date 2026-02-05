package kickstart

import (
	"bytes"
	"fmt"
	"text/template"
	"time"

	"github.com/lpmos/lpmos-go/pkg/models"
)

// Generator generates kickstart/preseed files
type Generator struct {
	templates map[string]*template.Template
}

// NewGenerator creates a new kickstart generator
func NewGenerator() *Generator {
	g := &Generator{
		templates: make(map[string]*template.Template),
	}
	g.loadTemplates()
	return g
}

// loadTemplates loads all templates
func (g *Generator) loadTemplates() {
	g.templates["centos-7"] = template.Must(template.New("centos-7").Parse(centos7Template))
	g.templates["centos-8"] = template.Must(template.New("centos-8").Parse(centos8Template))
	g.templates["rocky-8"] = template.Must(template.New("rocky-8").Parse(rocky8Template))
	g.templates["rocky-9"] = template.Must(template.New("rocky-9").Parse(rocky9Template))
	g.templates["ubuntu-20.04"] = template.Must(template.New("ubuntu-20.04").Parse(ubuntu2004Template))
	g.templates["ubuntu-22.04"] = template.Must(template.New("ubuntu-22.04").Parse(ubuntu2204Template))
}

// KickstartData represents data for kickstart template
type KickstartData struct {
	SN               string
	Hostname         string
	IP               string
	Netmask          string
	Gateway          string
	DNS              string
	PrimaryNIC       string
	RootPasswordHash string
	RepoURL          string
	OSType           string
	OSVersion        string
	BootDisk         string
	TargetDisks      string
	UseRAID          bool
	RAIDDisk         string
	RegionalURL      string
	DC               string
	Timestamp        string
	Packages         []string
	PostScript       string
}

// Generate generates kickstart/preseed content
func (g *Generator) Generate(task *models.TaskV3, config *models.OSInstallConfig) (string, error) {
	// 选择模板
	templateKey := fmt.Sprintf("%s-%s", config.OSType, config.OSVersion)
	tmpl, ok := g.templates[templateKey]
	if !ok {
		// 尝试使用主版本号
		templateKey = fmt.Sprintf("%s-%s", config.OSType, config.OSVersion[:1])
		tmpl, ok = g.templates[templateKey]
		if !ok {
			return "", fmt.Errorf("no template found for %s %s", config.OSType, config.OSVersion)
		}
	}

	// 准备数据
	data := &KickstartData{
		SN:               task.SN,
		Hostname:         task.Hostname,
		IP:               task.IP,
		Netmask:          config.Network.Netmask,
		Gateway:          config.Network.Gateway,
		DNS:              config.Network.DNS,
		PrimaryNIC:       config.Network.Interface,
		RootPasswordHash: config.RootPassword,
		RepoURL:          config.MirrorURL,
		OSType:           config.OSType,
		OSVersion:        config.OSVersion,
		RegionalURL:      config.RegionalURL, // Regional Client API URL
		DC:               task.SN,            // Data center from task
		Timestamp:        time.Now().Format("2006-01-02 15:04:05"),
		Packages:         config.Packages,
		PostScript:       config.PostScript,
	}

	// 磁盘配置
	if len(config.DiskLayout.Partitions) > 0 {
		data.BootDisk = config.DiskLayout.RootDisk
		data.TargetDisks = config.DiskLayout.RootDisk
	}

	// 执行模板
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// GeneratePreseed generates preseed for Debian/Ubuntu
func (g *Generator) GeneratePreseed(task *models.TaskV3, config *models.OSInstallConfig) (string, error) {
	return g.Generate(task, config)
}
