package resolver

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// builtinPorts is the fallback map for ports not found in /etc/services.
// Format: port number → "service-name (description)"
var builtinPorts = map[uint16]string{
	20:    "ftp-data (FTP Data Transfer)",
	21:    "ftp (File Transfer)",
	22:    "ssh (Secure Shell)",
	23:    "telnet (Telnet — unencrypted)",
	25:    "smtp (Email Sending)",
	53:    "dns (Domain Name System)",
	67:    "dhcp (DHCP Server)",
	68:    "dhcp (DHCP Client)",
	80:    "http (Web — unencrypted)",
	110:   "pop3 (Email Retrieval)",
	111:   "rpcbind (RPC Port Mapper)",
	123:   "ntp (Network Time Protocol)",
	143:   "imap (Email Access)",
	161:   "snmp (Network Management)",
	389:   "ldap (Directory Service)",
	443:   "https (Secure Web)",
	445:   "smb (Windows File Sharing)",
	465:   "smtps (Secure Email Sending)",
	514:   "syslog (System Logging)",
	587:   "submission (Email Submission)",
	631:   "ipp (Print Server)",
	636:   "ldaps (Secure Directory)",
	993:   "imaps (Secure Email Access)",
	995:   "pop3s (Secure Email Retrieval)",
	1080:  "socks (SOCKS Proxy)",
	1194:  "openvpn (VPN — OpenVPN)",
	1433:  "mssql (SQL Server Database)",
	1521:  "oracle (Oracle Database)",
	2375:  "docker (Docker API — unencrypted)",
	2376:  "docker-tls (Docker API — TLS)",
	2377:  "docker-swarm (Docker Swarm Management)",
	2379:  "etcd (etcd — key-value store)",
	2380:  "etcd-peer (etcd Peer Communication)",
	3000:  "grafana (Grafana Dashboard)",
	3306:  "mysql (MySQL Database)",
	3389:  "rdp (Remote Desktop)",
	4369:  "epmd (Erlang Port Mapper)",
	5000:  "docker-registry (Container Registry)",
	5432:  "postgresql (PostgreSQL Database)",
	5601:  "kibana (Kibana Dashboard)",
	5671:  "amqps (Secure Message Queue)",
	5672:  "amqp (Message Queue — AMQP)",
	6379:  "redis (Redis Cache)",
	6443:  "kubernetes (Kubernetes API Server)",
	7077:  "spark (Apache Spark)",
	8080:  "http-alt (Web — Alternate Port)",
	8443:  "https-alt (Secure Web — Alternate Port)",
	8888:  "jupyter (Jupyter Notebook)",
	9000:  "sonarqube (SonarQube / MinIO)",
	9090:  "prometheus (Prometheus Metrics)",
	9092:  "kafka (Apache Kafka)",
	9200:  "elasticsearch (Elasticsearch Search Engine)",
	9300:  "elasticsearch-node (Elasticsearch Node Communication)",
	10250: "kubelet (Kubernetes Node Agent)",
	15672: "rabbitmq-mgmt (RabbitMQ Management UI)",
	27017: "mongodb (MongoDB Database)",
	27018: "mongodb-shard (MongoDB Shard)",
	50070: "hdfs (Hadoop HDFS NameNode)",
}

// parsedServices holds ports loaded from /etc/services.
var parsedServices map[uint16]string

// InitPorts parses /etc/services and merges with the built-in map.
// The built-in map takes precedence for well-known ports.
// Safe to call multiple times; subsequent calls are no-ops.
func InitPorts() {
	if parsedServices != nil {
		return
	}
	parsedServices = make(map[uint16]string)

	f, err := os.Open("/etc/services")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		// port/proto e.g. "22/tcp"
		portProto := strings.SplitN(fields[1], "/", 2)
		if len(portProto) != 2 {
			continue
		}
		n, err := strconv.ParseUint(portProto[0], 10, 16)
		if err != nil {
			continue
		}
		port := uint16(n)
		if _, exists := parsedServices[port]; !exists {
			parsedServices[port] = name
		}
	}
}

// Port resolves a port number to "service-name (description)".
// If the port is unknown, it returns the raw ":port" string.
func Port(port uint16) string {
	// Built-in map has priority (richer descriptions).
	if label, ok := builtinPorts[port]; ok {
		return label
	}
	// Fall back to /etc/services.
	if parsedServices != nil {
		if name, ok := parsedServices[port]; ok {
			return fmt.Sprintf("%s (port %d)", name, port)
		}
	}
	// Unknown port — show raw.
	return fmt.Sprintf(":%d", port)
}

// PortWithNumber returns "service-name (description) — :port" for display
// in tables where the raw number is also useful context.
func PortWithNumber(port uint16) string {
	label := Port(port)
	if strings.HasPrefix(label, ":") {
		return label // already raw
	}
	return fmt.Sprintf("%s  ·  :%d", label, port)
}
