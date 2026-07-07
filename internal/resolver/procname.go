package resolver

import (
	"path/filepath"
	"strings"
)

// daemonNames maps well-known Linux daemon binary names to
// "name (description)" display strings.
var daemonNames = map[string]string{
	// Web servers
	"nginx":         "nginx (Web Server)",
	"apache2":       "apache2 (Apache HTTP Server)",
	"httpd":         "httpd (Apache HTTP Server)",
	"lighttpd":      "lighttpd (Web Server)",
	"caddy":         "caddy (Web Server)",

	// Databases
	"postgres":      "postgres (PostgreSQL Database)",
	"mysqld":        "mysqld (MySQL Database)",
	"mysqld_safe":   "mysqld_safe (MySQL Watchdog)",
	"mongod":        "mongod (MongoDB Database)",
	"redis-server":  "redis-server (Redis Cache)",
	"memcached":     "memcached (Memcached Cache)",
	"mariadbd":      "mariadbd (MariaDB Database)",

	// Messaging
	"kafka":         "kafka (Apache Kafka — Message Bus)",
	"zookeeper":     "zookeeper (ZooKeeper — Coordination)",
	"rabbitmq":      "rabbitmq (RabbitMQ — Message Queue)",

	// Container & orchestration
	"dockerd":       "dockerd (Docker Engine)",
	"containerd":    "containerd (Container Runtime)",
	"kubelet":       "kubelet (Kubernetes Node Agent)",
	"kube-proxy":    "kube-proxy (Kubernetes Network Proxy)",
	"etcd":          "etcd (etcd — Key-Value Store)",

	// SSH / remote access
	"sshd":          "sshd (SSH Daemon)",
	"dropbear":      "dropbear (Lightweight SSH)",

	// System services
	"systemd":       "systemd (System Manager)",
	"systemd-journald": "systemd-journald (Log Collector)",
	"systemd-resolved": "systemd-resolved (DNS Resolver)",
	"systemd-networkd": "systemd-networkd (Network Manager)",
	"systemd-udevd": "systemd-udevd (Device Manager)",
	"dbus-daemon":   "dbus-daemon (D-Bus Message Bus)",
	"NetworkManager": "NetworkManager (Network Manager)",
	"wpa_supplicant": "wpa_supplicant (Wi-Fi Authentication)",
	"chronyd":       "chronyd (Network Time — Chrony)",
	"ntpd":          "ntpd (Network Time Protocol)",
	"crond":         "crond (Cron Scheduler)",
	"cron":          "cron (Cron Scheduler)",
	"atd":           "atd (At Job Scheduler)",
	"rsyslogd":      "rsyslogd (System Log Daemon)",
	"syslogd":       "syslogd (System Log Daemon)",

	// Security
	"sshguard":      "sshguard (SSH Brute-Force Guard)",
	"fail2ban":      "fail2ban (Brute-Force Protection)",
	"ufw":           "ufw (Uncomplicated Firewall)",
	"iptables":      "iptables (Firewall)",
	"auditd":        "auditd (Audit Daemon)",

	// Kernel threads
	"kthreadd":      "kthreadd (Kernel Thread Manager)",
	"ksoftirqd":     "ksoftirqd (Kernel Soft IRQ)",
	"kworker":       "kworker (Kernel Worker)",
	"migration":     "migration (CPU Migration Thread)",
	"rcu_sched":     "rcu_sched (RCU Scheduler)",
	"rcu_bh":        "rcu_bh (RCU Bottom Half)",
	"watchdog":      "watchdog (Kernel Watchdog)",

	// Runtimes
	"java":          "java (Java Runtime)",
	"python":        "python (Python Runtime)",
	"python3":       "python3 (Python 3 Runtime)",
	"ruby":          "ruby (Ruby Runtime)",
	"node":          "node (Node.js Runtime)",
	"perl":          "perl (Perl Runtime)",
	"php":           "php (PHP CLI)",
	"php-fpm":       "php-fpm (PHP FastCGI)",
	"php-fpm8":      "php-fpm8 (PHP 8 FastCGI)",
	"go":            "go (Go Runtime)",

	// Monitoring
	"prometheus":    "prometheus (Prometheus Metrics)",
	"grafana":       "grafana (Grafana Dashboard)",
	"node_exporter": "node_exporter (Prometheus Node Metrics)",
	"alertmanager":  "alertmanager (Prometheus Alerting)",

	// Search
	"elasticsearch": "elasticsearch (Elasticsearch Search)",
	"kibana":        "kibana (Kibana Dashboard)",
	"logstash":      "logstash (Logstash Log Pipeline)",
}

// ProcName resolves a process name to a human-readable display string.
// kernelName is the truncated name from /proc/<pid>/status (max 15 chars).
// cmdline is the full command line from /proc/<pid>/cmdline (may be empty).
//
// Resolution priority:
//  1. Built-in daemon map on kernelName
//  2. Built-in daemon map on the binary basename from cmdline
//  3. kernelName as-is (already descriptive enough)
func ProcName(kernelName, cmdline string) string {
	// Normalise the kernel name (strip common suffixes like trailing digits).
	normalized := strings.TrimRight(kernelName, "0123456789")

	// 1. Try the full kernel name.
	if label, ok := daemonNames[kernelName]; ok {
		return label
	}
	// 2. Try the normalised kernel name (e.g. "kworker/0:1" → "kworker").
	baseName := strings.SplitN(normalized, "/", 2)[0]
	if label, ok := daemonNames[baseName]; ok {
		return label
	}

	// 3. Try the binary name from the full command line.
	if cmdline != "" {
		// cmdline fields are NUL-separated; first field is the executable path.
		exe := strings.SplitN(cmdline, "\x00", 2)[0]
		exe = filepath.Base(exe)
		// Strip version suffix e.g. "python3.11" → "python3"
		exeBase := strings.TrimRight(exe, "0123456789.")
		if label, ok := daemonNames[exe]; ok {
			return label
		}
		if label, ok := daemonNames[exeBase]; ok {
			return label
		}
	}

	// 4. Return the kernel name unchanged — it's already descriptive enough.
	return kernelName
}
