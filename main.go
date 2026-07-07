// BlackEye — Linux System Administration Dashboard
// Entry point: wires configuration, all microservices, event bus,
// service registry, and the bubbletea TUI together.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"blackeye/internal/bus"
	"blackeye/internal/config"
	"blackeye/internal/privilege"
	"blackeye/internal/registry"
	"blackeye/internal/resolver"
	"blackeye/internal/ui"

	auditsvc  "blackeye/internal/services/audit"
	cpusvc    "blackeye/internal/services/cpu"
	disksvc   "blackeye/internal/services/disk"
	dmesgsvc  "blackeye/internal/services/dmesg"
	dockersvc "blackeye/internal/services/docker"
	iosvc     "blackeye/internal/services/io"
	memsvc    "blackeye/internal/services/memory"
	netsvc    "blackeye/internal/services/network"
	netstatsvc "blackeye/internal/services/netstats"
	portssvc  "blackeye/internal/services/ports"
	routingsvc "blackeye/internal/services/routing"
	swapsvc   "blackeye/internal/services/swap"
	sysinfosvc "blackeye/internal/services/sysinfo"
	systemdsvc "blackeye/internal/services/systemd"
	thermalsvc "blackeye/internal/services/thermal"
	procsvc   "blackeye/internal/services/process"

	uitabs "blackeye/internal/ui/tabs"
)

func main() {
	// 1. Detect privileges.
	privilege.Init()

	// 2. Load configuration (uses defaults if no config file exists).
	cfgPath := config.DefaultPath
	if p := os.Getenv("BLACKEYE_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "blackeye: config error: %v\n", err)
		os.Exit(1)
	}

	// 3. Initialise resolvers (one-time parsing).
	resolver.InitPorts()
	if err := resolver.InitUsers(); err != nil {
		fmt.Fprintf(os.Stderr, "blackeye: warning: cannot parse /etc/passwd: %v\n", err)
	}

	// 4. Create the event bus.
	b := bus.New()

	// 5. Create all services.
	audit := auditsvc.New(cfg)
	cpu := cpusvc.New(cfg)
	mem := memsvc.New(cfg)
	swap := swapsvc.New(cfg)
	disk := disksvc.New(cfg)
	ioSvc := iosvc.New(cfg)
	net := netsvc.New(cfg)
	netstats := netstatsvc.New(cfg)
	routing := routingsvc.New(cfg)
	thermal := thermalsvc.New(cfg)
	sysinfo := sysinfosvc.New(cfg)
	proc := procsvc.New(cfg)
	ports := portssvc.New(cfg)
	docker := dockersvc.New(cfg, false)
	systemd := systemdsvc.New(cfg)
	dmesg := dmesgsvc.New(cfg)

	// 6. Register and start all services.
	reg := registry.New(b)
	reg.Register(cpu)
	reg.Register(mem)
	reg.Register(swap)
	reg.Register(disk)
	reg.Register(ioSvc)
	reg.Register(net)
	reg.Register(netstats)
	reg.Register(routing)
	reg.Register(thermal)
	reg.Register(sysinfo)
	reg.Register(proc)
	reg.Register(ports)
	reg.Register(docker)
	reg.Register(systemd)
	reg.Register(dmesg)

	ctx, cancelSvcs := context.WithCancel(context.Background())
	defer cancelSvcs()

	// Start audit service separately (it doesn't publish to the bus).
	auditCtx, cancelAudit := context.WithCancel(ctx)
	defer cancelAudit()
	go func() { _ = audit.Start(auditCtx) }()

	// Start all data-collection services.
	reg.StartAll(ctx)

	// 7. Build the TUI root model.
	root := ui.New(b, cfg)

	// Wire audit service into tabs that perform destructive actions.
	if procTab, ok := root.GetTab(ui.TabProcess).(*uitabs.Process); ok {
		procTab.SetAudit(audit)
	}
	if dockerTab, ok := root.GetTab(ui.TabDocker).(*uitabs.Docker); ok {
		dockerTab.SetAudit(audit)
	}

	// 8. Run the TUI.
	p := tea.NewProgram(root,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Handle OS signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "blackeye: TUI error: %v\n", err)
		cancelSvcs()
		reg.StopAll()
		os.Exit(1)
	}

	// 9. Graceful shutdown.
	cancelSvcs()
	reg.StopAll()
	b.Close()
}
