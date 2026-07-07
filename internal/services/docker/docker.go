// Package docker communicates with the Docker Engine via its Unix socket.
// It uses the official Docker Go SDK. No external docker CLI is used.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"blackeye/internal/config"
	"blackeye/internal/resolver"
	"blackeye/internal/services"
)

// sensitiveEnvKeys are substrings that trigger value redaction.
var sensitiveEnvKeys = []string{
	"PASSWORD", "SECRET", "TOKEN", "KEY", "CREDENTIAL", "AUTH", "PRIVATE",
}

// EnvVar holds one environment variable, potentially redacted.
type EnvVar struct {
	Key      string
	Value    string
	Redacted bool
}

// ContainerInfo holds display-ready data for one container.
type ContainerInfo struct {
	ID            string
	Name          string
	Image         string
	DisplayStatus string  // "running (Active)"
	Uptime        string  // "3 days ago" / "Today 09:12"
	CPUPercent    float64
	MemDisplay    string  // "142.2 MiB / 7.8 GiB"
	Ports         []string
	// Detail panel:
	FullID      string
	EnvVars     []EnvVar
	Mounts      []string
	NetworkInfo string
	Labels      map[string]string
}

// Snapshot is the published payload.
type Snapshot struct {
	Containers []ContainerInfo
	Available  bool   // false if Docker socket is unreachable
	Error      string // human-readable error when Available=false
	Timestamp  time.Time
}

// Service collects Docker container data.
type Service struct {
	interval    time.Duration
	showSecrets bool
	out         chan interface{}
	health      atomic.Value
	cancel      context.CancelFunc
	cli         *client.Client
}

func New(cfg config.Config, showSecrets bool) *Service {
	s := &Service{
		interval:    time.Duration(cfg.Refresh.DockerInterval) * time.Second,
		showSecrets: showSecrets,
		out:         make(chan interface{}, 4),
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})
	return s
}

func (s *Service) Name()   string                { return "Docker Monitor" }
func (s *Service) Topic()  string                { return "docker" }
func (s *Service) Output() <-chan interface{}     { return s.out }
func (s *Service) Health() services.HealthStatus { return s.health.Load().(services.HealthStatus) }
func (s *Service) Stop()   { if s.cancel != nil { s.cancel() } }
func (s *Service) Reload(cfg config.Config) {
	s.interval = time.Duration(cfg.Refresh.DockerInterval) * time.Second
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)

	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		snap := Snapshot{
			Available: false,
			Error:     fmt.Sprintf("Cannot connect to Docker: %v\nEnsure /var/run/docker.sock is accessible.\nRun: sudo usermod -aG docker $USER", err),
			Timestamp: time.Now(),
		}
		s.health.Store(services.HealthStatus{State: services.HealthDown, Reason: err.Error()})
		select {
		case s.out <- snap:
		default:
		}
		<-ctx.Done()
		return nil
	}
	s.cli = cli
	defer cli.Close()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			snap := s.collect(ctx)
			select {
			case s.out <- snap:
			default:
			}
		}
	}
}

func (s *Service) collect(ctx context.Context) Snapshot {
	containers, err := s.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		s.health.Store(services.HealthStatus{State: services.HealthDown, Reason: err.Error()})
		return Snapshot{
			Available: false,
			Error:     "Docker connection lost: " + err.Error(),
			Timestamp: time.Now(),
		}
	}
	s.health.Store(services.HealthStatus{State: services.HealthOK})

	var infos []ContainerInfo
	for _, c := range containers {
		info := s.buildInfo(ctx, c)
		infos = append(infos, info)
	}
	return Snapshot{Containers: infos, Available: true, Timestamp: time.Now()}
}

func (s *Service) buildInfo(ctx context.Context, c container.Summary) ContainerInfo {
	name := strings.TrimPrefix(c.Names[0], "/")
	status := resolver.DockerStatus(string(c.State))

	// CPU and memory from stats (non-streaming single read).
	var cpuPct float64
	var memDisplay string
	statsResp, err := s.cli.ContainerStatsOneShot(ctx, c.ID)
	if err == nil {
		defer statsResp.Body.Close()
		var stats container.StatsResponse
		body, _ := io.ReadAll(statsResp.Body)
		if json.Unmarshal(body, &stats) == nil {
			cpuPct = calcCPUPercent(&stats)
			memDisplay = fmt.Sprintf("%s / %s",
				resolver.FormatBytes(stats.MemoryStats.Usage),
				resolver.FormatBytes(stats.MemoryStats.Limit),
			)
		}
	}
	if memDisplay == "" {
		memDisplay = "—"
	}

	// Port mappings.
	var ports []string
	for _, p := range c.Ports {
		if p.PublicPort > 0 {
			ports = append(ports, fmt.Sprintf("%s → %d", resolver.Port(p.PublicPort), p.PrivatePort))
		} else {
			ports = append(ports, resolver.Port(p.PrivatePort))
		}
	}

	// Uptime.
	uptime := formatUptime(c.Created)

	// Detailed info (inspect).
	var envVars []EnvVar
	var mounts []string
	var networkInfo string
	var labels map[string]string
	fullID := c.ID

	inspect, err := s.cli.ContainerInspect(ctx, c.ID)
	if err == nil {
		fullID = inspect.ID
		labels = inspect.Config.Labels
		for _, e := range inspect.Config.Env {
			parts := strings.SplitN(e, "=", 2)
			key := parts[0]
			val := ""
			if len(parts) == 2 {
				val = parts[1]
			}
			redacted := !s.showSecrets && isSensitive(key)
			if redacted {
				val = "[REDACTED]"
			}
			envVars = append(envVars, EnvVar{Key: key, Value: val, Redacted: redacted})
		}
		for _, m := range inspect.Mounts {
			mounts = append(mounts, fmt.Sprintf("%s → %s", m.Source, m.Destination))
		}
		if n := inspect.NetworkSettings; n != nil {
			var nets []string
			for netName, ep := range n.Networks {
				if ep != nil {
					nets = append(nets, fmt.Sprintf("%s (IP: %s, GW: %s)", netName, ep.IPAddress, ep.Gateway))
				}
			}
			networkInfo = strings.Join(nets, "; ")
		}
	}

	return ContainerInfo{
		ID:            c.ID[:12],
		Name:          name,
		Image:         c.Image,
		DisplayStatus: status,
		Uptime:        uptime,
		CPUPercent:    cpuPct,
		MemDisplay:    memDisplay,
		Ports:         ports,
		FullID:        fullID,
		EnvVars:       envVars,
		Mounts:        mounts,
		NetworkInfo:   networkInfo,
		Labels:        labels,
	}
}

func calcCPUPercent(stats *container.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) -
		float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) -
		float64(stats.PreCPUStats.SystemUsage)
	if systemDelta == 0 {
		return 0
	}
	numCPU := float64(stats.CPUStats.OnlineCPUs)
	if numCPU == 0 {
		numCPU = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	return (cpuDelta / systemDelta) * numCPU * 100
}

func isSensitive(key string) bool {
	upper := strings.ToUpper(key)
	for _, s := range sensitiveEnvKeys {
		if strings.Contains(upper, s) {
			return true
		}
	}
	return false
}

func formatUptime(created int64) string {
	t := time.Unix(created, 0)
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "Just started"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hour(s) ago", int(d.Hours()))
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%d day(s) ago", days)
	}
}
