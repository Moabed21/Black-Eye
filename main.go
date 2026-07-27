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
	"blackeye/internal/sysdetect"
	"blackeye/internal/ui"

	auditsvc  "blackeye/internal/services/audit"
	alertssvc "blackeye/internal/services/alerts"
	advancedsvc "blackeye/internal/services/advanced"
	cpusvc    "blackeye/internal/services/cpu"
	disksvc   "blackeye/internal/services/disk"
	dmesgsvc  "blackeye/internal/services/dmesg"
	dockersvc "blackeye/internal/services/docker"
	firewallsvc "blackeye/internal/services/firewall"
	initsyssvc "blackeye/internal/services/initsys"
	iosvc     "blackeye/internal/services/io"
	memsvc    "blackeye/internal/services/memory"
	netsvc    "blackeye/internal/services/network"
	netstatsvc "blackeye/internal/services/netstats"
	pkgsvc    "blackeye/internal/services/packages"
	portssvc  "blackeye/internal/services/ports"
	routingsvc "blackeye/internal/services/routing"
	swapsvc   "blackeye/internal/services/swap"
	securitysvc "blackeye/internal/services/security"
	sysinfosvc "blackeye/internal/services/sysinfo"
	systemdsvc "blackeye/internal/services/systemd"
	thermalsvc "blackeye/internal/services/thermal"
	usersvc   "blackeye/internal/services/users"
	procsvc   "blackeye/internal/services/process"

	uitabs "blackeye/internal/ui/tabs"
)

func main() {
	// 1. Detect privileges and system environment.
	privilege.Init()
	sysdetect.Detect()

	// Log detected environment for debugging.
	if os.Getenv("BLACKEYE_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "blackeye: %s\n", sysdetect.Profile().String())
	}

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

	// Check CLI export / health mode
	for _, arg := range os.Args[1:] {
		if arg == "--export" || arg == "--export=json" || arg == "--health" {
			h, _ := os.Hostname()
			fmt.Printf("{\n  \"version\": \"1.3.0\",\n  \"status\": \"healthy\",\n  \"hostname\": %q,\n  \"distro\": %q\n}\n", h, sysdetect.Profile().DistroName)
			return
		}
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
	initsys := initsyssvc.New(cfg)
	fw := firewallsvc.New(cfg)
	pkgs := pkgsvc.New(cfg)
	usrSvc := usersvc.New(cfg)
	advSvc := advancedsvc.New(cfg)
	secSvc := securitysvc.New(cfg)
	dmesg := dmesgsvc.New(cfg)
	alertsMon := alertssvc.New(b, cfg)

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
	reg.Register(initsys)
	reg.Register(fw)
	reg.Register(pkgs)
	reg.Register(usrSvc)
	reg.Register(advSvc)
	reg.Register(secSvc)
	reg.Register(dmesg)

	ctx, cancelSvcs := context.WithCancel(context.Background())
	defer cancelSvcs()

	// Start alerts monitor in its own goroutine (it subscribes to bus topics
	// directly and publishes alerts).
	alertsCtx, cancelAlerts := context.WithCancel(ctx)
	defer cancelAlerts()
	go func() { _ = alertsMon.Start(alertsCtx) }()

	// Start audit service separately (it doesn't publish to the bus).
	auditCtx, cancelAudit := context.WithCancel(ctx)
	defer cancelAudit()
	go func() { _ = audit.Start(auditCtx) }()

	// Start all data-collection services.
	reg.StartAll(ctx)

	// 7. Build the TUI root model.
	root := ui.New(b, cfg)

	// Sub-command / CLI tab focus parsing
	for _, arg := range os.Args[1:] {
		switch arg {
		case "net", "--tab=network":
			root.SetActiveTab(ui.TabNetwork)
		case "sec", "--tab=security":
			root.SetActiveTab(ui.TabUsers)
		case "pkg", "--tab=packages":
			root.SetActiveTab(ui.TabPackages)
		case "proc", "--tab=process":
			root.SetActiveTab(ui.TabProcess)
		case "term", "--tab=terminal":
			root.SetActiveTab(ui.TabTerminal)
		case "fw", "--tab=firewall":
			root.SetActiveTab(ui.TabFirewall)
		}
	}

	// Wire audit service into tabs that perform destructive actions.
	if procTab, ok := root.GetTab(ui.TabProcess).(*uitabs.Process); ok {
		procTab.SetAudit(audit)
	}
	if dockerTab, ok := root.GetTab(ui.TabDocker).(*uitabs.Docker); ok {
		dockerTab.SetDocker(docker)
		dockerTab.SetAudit(audit)
	}
	if servicesTab, ok := root.GetTab(ui.TabServices).(*uitabs.Services); ok {
		servicesTab.SetSystemd(systemd)
		servicesTab.SetInitSys(initsys)
		servicesTab.SetAudit(audit)
	}
	if fwTab, ok := root.GetTab(ui.TabFirewall).(*uitabs.Firewall); ok {
		fwTab.SetFirewall(fw)
		fwTab.SetAudit(audit)
	}
	if pkgTab, ok := root.GetTab(ui.TabPackages).(*uitabs.Packages); ok {
		pkgTab.SetPackages(pkgs)
		pkgTab.SetAudit(audit)
	}
	if usrTab, ok := root.GetTab(ui.TabUsers).(*uitabs.Users); ok {
		usrTab.SetUsers(usrSvc)
		usrTab.SetAudit(audit)
	}
	if advTab, ok := root.GetTab(ui.TabAdvanced).(*uitabs.Advanced); ok {
		advTab.SetAdvanced(advSvc)
		advTab.SetAudit(audit)
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
