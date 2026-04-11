// Command gendemo writes a SQLite file with realistic sample data for UI testing.
//
//	go run ./cmd/gendemo -o demo.db
package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/keys"
	"github.com/eterm/eterm/internal/security"
)

func main() {
	out := flag.String("o", "demo.db", "output SQLite database path")
	flag.Parse()

	path, err := filepath.Abs(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	_ = os.Remove(path)

	database, err := db.InitDB(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: init db: %v\n", err)
		os.Exit(1)
	}

	mkm := security.NewMasterKeyManager(nil, nil, 30*time.Minute)
	salt, verifier := mkm.SetupNoPassword()
	if err := db.SetSetting(database, "encryption_salt", base64.StdEncoding.EncodeToString(salt)); err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}
	if err := db.SetSetting(database, "encryption_verifier", base64.StdEncoding.EncodeToString(verifier)); err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}
	if err := db.SetSetting(database, "no_password", "true"); err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	keyBytes := mkm.GetKey().Bytes()
	encrypt := func(plain string) (string, error) {
		return security.Encrypt([]byte(plain), keyBytes)
	}

	priv1, pub1, fp1, err := keys.GenerateED25519()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}
	keyWork := db.SSHKey{
		Name:           "MacBook · ed25519",
		Type:           "ed25519",
		PrivateKeyData: string(priv1),
		PublicKeyData:  pub1,
		Fingerprint:    fp1,
		Bits:           256,
		StorageMode:    "database",
	}
	if err := database.Create(&keyWork).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	priv2, pub2, fp2, err := keys.GenerateED25519()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}
	keyCI := db.SSHKey{
		Name:           "CI · deploy key",
		Type:           "ed25519",
		PrivateKeyData: string(priv2),
		PublicKeyData:  pub2,
		Fingerprint:    fp2,
		Bits:           256,
		StorageMode:    "database",
	}
	if err := database.Create(&keyCI).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	priv3, pub3, fp3, err := keys.GenerateRSA(2048)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}
	keyLegacy := db.SSHKey{
		Name:           "Legacy build · RSA 2048",
		Type:           "rsa",
		PrivateKeyData: string(priv3),
		PublicKeyData:  pub3,
		Fingerprint:    fp3,
		Bits:           2048,
		StorageMode:    "database",
	}
	if err := database.Create(&keyLegacy).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	kwID := keyWork.ID
	ciID := keyCI.ID
	legID := keyLegacy.ID

	bastion := db.Host{
		Alias:       "prod-bastion",
		Hostname:    "bastion.acme.internal",
		Port:        22,
		Username:    "ec2-user",
		AuthMethod:  "key",
		KeyID:       &kwID,
		Group:       "production",
		Tags:        "aws,ssm",
		Description: "SSH entry for VPC 10.0.0.0/16; MFA at VPN layer.",
	}
	t1 := time.Date(2026, 4, 9, 14, 22, 0, 0, time.Local)
	bastion.LastConnectedAt = &t1
	if err := database.Create(&bastion).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	stgBastion := db.Host{
		Alias:       "stg-bastion",
		Hostname:    "stg-jump.acme.io",
		Port:        22,
		Username:    "ubuntu",
		AuthMethod:  "key",
		KeyID:       &kwID,
		Group:       "staging",
		Tags:        "aws,jump",
		Description: "Staging VPC entry; security group allows office IP only.",
	}
	tStgB := time.Date(2026, 4, 10, 10, 0, 0, 0, time.Local)
	stgBastion.LastConnectedAt = &tStgB
	if err := database.Create(&stgBastion).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	bJump := bastion.ID
	stgJump := stgBastion.ID

	appVPC := db.Host{
		Alias:       "app-api-prod",
		Hostname:    "10.0.12.41",
		Port:        22,
		Username:    "deploy",
		AuthMethod:  "key",
		KeyID:       &ciID,
		JumpHostID:  &bastion.ID,
		Group:       "production",
		Tags:        "k8s,api",
		Description: "Node pool workers; kubectl via SSM port-forward from bastion.",
	}
	t2 := time.Date(2026, 4, 10, 9, 5, 0, 0, time.Local)
	appVPC.LastConnectedAt = &t2
	if err := database.Create(&appVPC).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	stagingPW, err := encrypt("Stg#2026-demo")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}
	staging := db.Host{
		Alias:       "staging-web",
		Hostname:    "stg-web-01.acme.io",
		Port:        22,
		Username:    "ubuntu",
		AuthMethod:  "password",
		Password:    stagingPW,
		Group:       "staging",
		Tags:        "nginx,docker",
		Description: "Password rotated monthly; HTTP basic in front of app.",
	}
	if err := database.Create(&staging).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	hetzner := db.Host{
		Alias:       "hetzner-build",
		Hostname:    "build-01.eu.acme.io",
		Port:        22,
		Username:    "root",
		AuthMethod:  "key",
		KeyID:       &legID,
		Group:       "ci",
		Tags:        "gitlab-runner",
		Description: "Self-hosted runners; outbound only to registry and apt mirrors.",
	}
	if err := database.Create(&hetzner).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	homeLab := db.Host{
		Alias:       "homelab-nas",
		Hostname:    "192.168.1.88",
		Port:        2222,
		Username:    "admin",
		AuthMethod:  "key",
		KeyID:       &kwID,
		Group:       "personal",
		Tags:        "synology,tailscale",
		Description: "LAN only; Tailscale subnet router for remote access.",
	}
	t3 := time.Date(2026, 4, 8, 21, 40, 0, 0, time.Local)
	homeLab.LastConnectedAt = &t3
	if err := database.Create(&homeLab).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	proxyPW, err := encrypt("CorpProxy!demo")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}
	corpProxy := db.Host{
		Alias:         "vendor-sftp",
		Hostname:      "sftp.partner-vendor.com",
		Port:          22,
		Username:      "acme_upload",
		AuthMethod:    "key",
		KeyID:         &ciID,
		Group:         "integrations",
		Tags:          "sftp,edi",
		Description:   "HTTP CONNECT proxy required from office network.",
		ProxyType:     "http",
		ProxyHost:     "proxy.corp.acme.com",
		ProxyPort:     8080,
		ProxyUser:     "huangzheng",
		ProxyPassword: proxyPW,
	}
	if err := database.Create(&corpProxy).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	agentHost := db.Host{
		Alias:       "dev-mac-local",
		Hostname:    "127.0.0.1",
		Port:        22,
		Username:    os.Getenv("USER"),
		AuthMethod:  "agent",
		Group:       "personal",
		Tags:        "ssh-agent",
		Description: "Uses Keychain-backed ssh-agent; for quick local tests.",
	}
	if agentHost.Username == "" {
		agentHost.Username = "developer"
	}
	if err := database.Create(&agentHost).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	dbHost := db.Host{
		Alias:       "rds-tunnel-host",
		Hostname:    "10.0.20.5",
		Port:        22,
		Username:    "ec2-user",
		AuthMethod:  "key",
		KeyID:       &kwID,
		JumpHostID:  &bastion.ID,
		Group:       "production",
		Tags:        "postgres,bastion",
		Description: "EC2 with SSM; use local port forward to RDS security group.",
	}
	if err := database.Create(&dbHost).Error; err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	demoSharedPW, err := encrypt("Shared-demo#2026")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
		os.Exit(1)
	}

	extraHosts := []db.Host{
		// production (via prod bastion)
		{Alias: "prod-web-01", Hostname: "10.0.12.50", Port: 22, Username: "deploy", AuthMethod: "key", KeyID: &ciID, JumpHostID: &bJump, Group: "production", Tags: "nginx,k8s", Description: "Ingress pool node az-1a."},
		{Alias: "prod-web-02", Hostname: "10.0.12.51", Port: 22, Username: "deploy", AuthMethod: "key", KeyID: &ciID, JumpHostID: &bJump, Group: "production", Tags: "nginx,k8s", Description: "Ingress pool node az-1b."},
		{Alias: "prod-worker-batch", Hostname: "10.0.14.20", Port: 22, Username: "batch", AuthMethod: "key", KeyID: &ciID, JumpHostID: &bJump, Group: "production", Tags: "celery,sidekiq", Description: "Queue workers; autoscale 2–12."},
		{Alias: "prod-redis", Hostname: "10.0.15.8", Port: 22, Username: "redis", AuthMethod: "key", KeyID: &kwID, JumpHostID: &bJump, Group: "production", Tags: "redis,sentinel", Description: "Primary + replica; no public."},
		{Alias: "prod-rabbitmq", Hostname: "10.0.16.3", Port: 22, Username: "ubuntu", AuthMethod: "key", KeyID: &ciID, JumpHostID: &bJump, Group: "production", Tags: "rabbitmq", Description: "AMQP cluster; shovel to DR region."},
		{Alias: "obs-grafana", Hostname: "10.0.18.40", Port: 22, Username: "grafana", AuthMethod: "key", KeyID: &kwID, JumpHostID: &bJump, Group: "production", Tags: "prometheus,grafana", Description: "Dashboards; SSO via Okta."},
		{Alias: "obs-loki", Hostname: "10.0.18.41", Port: 22, Username: "ubuntu", AuthMethod: "key", KeyID: &kwID, JumpHostID: &bJump, Group: "production", Tags: "loki,logs", Description: "Log retention 14d hot, 90d cold."},
		{Alias: "clickhouse-olap", Hostname: "10.0.19.7", Port: 22, Username: "clickhouse", AuthMethod: "key", KeyID: &legID, JumpHostID: &bJump, Group: "production", Tags: "clickhouse,analytics", Description: "Product analytics; read-only analysts."},
		{Alias: "prod-sftp-bridge", Hostname: "10.0.22.2", Port: 22, Username: "sftp", AuthMethod: "key", KeyID: &ciID, JumpHostID: &bJump, Group: "production", Tags: "sftp,partner", Description: "Inbound file drops from finance partners."},
		// staging (via stg bastion)
		{Alias: "stg-api", Hostname: "172.16.8.10", Port: 22, Username: "deploy", AuthMethod: "key", KeyID: &ciID, JumpHostID: &stgJump, Group: "staging", Tags: "api,rails", Description: "Feature branch deploys from GitHub Actions."},
		{Alias: "stg-worker", Hostname: "172.16.8.20", Port: 22, Username: "deploy", AuthMethod: "key", KeyID: &ciID, JumpHostID: &stgJump, Group: "staging", Tags: "sidekiq", Description: "Shares Redis with stg-redis."},
		{Alias: "stg-redis", Hostname: "172.16.9.5", Port: 22, Username: "ubuntu", AuthMethod: "key", KeyID: &kwID, JumpHostID: &stgJump, Group: "staging", Tags: "redis", Description: "Single node; flushed nightly."},
		{Alias: "stg-mysql", Hostname: "172.16.10.3", Port: 22, Username: "dba", AuthMethod: "password", Password: demoSharedPW, JumpHostID: &stgJump, Group: "staging", Tags: "mysql", Description: "MySQL 8; snapshot from anonymized prod weekly."},
		// other clouds / direct
		{Alias: "gcp-bastion", Hostname: "34.12.88.101", Port: 22, Username: "huangzheng", AuthMethod: "key", KeyID: &kwID, Group: "gcp", Tags: "iap,bastion", Description: "GCP OS Login + IAP tunnel; project acme-prod-2."},
		{Alias: "gcp-gke-node", Hostname: "10.128.0.14", Port: 22, Username: "containerd", AuthMethod: "key", KeyID: &ciID, Group: "gcp", Tags: "gke", Description: "Debug pod networking; use kubectl debug first."},
		{Alias: "azure-vm-dev", Hostname: "acme-dev.eastus.cloudapp.azure.com", Port: 22, Username: "azureuser", AuthMethod: "key", KeyID: &kwID, Group: "azure", Tags: "dev,vm", Description: "Standard_D4s_v5; spot when possible."},
		{Alias: "oci-arm-runner", Hostname: "152.67.44.201", Port: 22, Username: "opc", AuthMethod: "key", KeyID: &legID, Group: "oci", Tags: "arm,ci", Description: "Ampere A1; builds linux/arm64 images."},
		{Alias: "lightsail-blog", Hostname: "blog.acme.io", Port: 22, Username: "ubuntu", AuthMethod: "key", KeyID: &kwID, Group: "misc", Tags: "wordpress,lightsail", Description: "Marketing static + WP; Cloudflare in front."},
		{Alias: "qa-all-in-one", Hostname: "qa.acme.internal", Port: 22, Username: "qa", AuthMethod: "password", Password: demoSharedPW, Group: "qa", Tags: "tomcat,legacy", Description: "Pre-prod QA; shared creds for testers."},
		{Alias: "vpn-gateway", Hostname: "192.168.0.1", Port: 22, Username: "root", AuthMethod: "password", Password: demoSharedPW, Group: "network", Tags: "openwrt,vpn", Description: "Site router; SSH only from LAN."},
		{Alias: "docker-host-dev", Hostname: "192.168.64.3", Port: 22, Username: "docker", AuthMethod: "key", KeyID: &kwID, Group: "dev", Tags: "docker,colima", Description: "Local VM bridge to macOS Docker Desktop."},
		{Alias: "pypi-mirror", Hostname: "pypi.internal.acme.com", Port: 22, Username: "pypi", AuthMethod: "key", KeyID: &ciID, Group: "ci", Tags: "devpi,cache", Description: "Internal PyPI mirror; sync from upstream hourly."},
		{Alias: "registry-mirror", Hostname: "registry.internal.acme.com", Port: 22, Username: "registry", AuthMethod: "key", KeyID: &ciID, Group: "ci", Tags: "harbor,docker", Description: "Harbor HA; image scanning enabled."},
		{Alias: "dr-bastion", Hostname: "dr-bastion.acme-disaster.io", Port: 22022, Username: "ec2-user", AuthMethod: "key", KeyID: &kwID, Group: "dr", Tags: "aws,backup", Description: "Warm standby region; DNS failover manual."},
		{Alias: "windows-jump", Hostname: "jump-win.corp.acme.com", Port: 22, Username: "Administrator", AuthMethod: "password", Password: demoSharedPW, Group: "corp", Tags: "rdp,jump", Description: "OpenSSH on Windows Server; RDP to desktops behind."},
	}
	for i := range extraHosts {
		t := time.Date(2026, 4, int(1+(i%10)), 12+(i%8), (i*7)%60, 0, 0, time.Local)
		extraHosts[i].LastConnectedAt = &t
		if err := database.Create(&extraHosts[i]).Error; err != nil {
			fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
			os.Exit(1)
		}
	}

	fingerprints := []db.HostFingerprint{
		{Hostname: "bastion.acme.internal", Port: 22, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:WVXL3dK8mNqQpR1sT2uV4wX5yZ6aB7cD8eF9gH0iJ1k", TrustedAt: time.Date(2025, 11, 3, 10, 0, 0, 0, time.UTC)},
		{Hostname: "stg-jump.acme.io", Port: 22, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:StgJmp0123456789AbCdEfGhIjKlMnOpQrStUvWxYzABCDE", TrustedAt: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)},
		{Hostname: "stg-web-01.acme.io", Port: 22, Algorithm: "ecdsa-sha2-nistp256", Fingerprint: "SHA256:AbCdEfGhIjKlMnOpQrStUvWxYz0123456789abcdefghij", TrustedAt: time.Date(2026, 1, 15, 8, 30, 0, 0, time.UTC)},
		{Hostname: "build-01.eu.acme.io", Port: 22, Algorithm: "ssh-rsa", Fingerprint: "SHA256:ZzYyXxWwVvUuTtSsRrQqPpOoNnMmLlKkJjIiHhGgFfEeDd", TrustedAt: time.Date(2024, 7, 22, 12, 0, 0, 0, time.UTC)},
		{Hostname: "34.12.88.101", Port: 22, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:GcpBst9876543210ZyXwVuTsRqPoNmLkJiHgFeDcBa987654", TrustedAt: time.Date(2026, 3, 20, 6, 15, 0, 0, time.UTC)},
		{Hostname: "dr-bastion.acme-disaster.io", Port: 22022, Algorithm: "ecdsa-sha2-nistp384", Fingerprint: "SHA256:DrSite0123456789ABCDEFGHijklmnopqrstuvwxyz0123456", TrustedAt: time.Date(2025, 8, 10, 0, 0, 0, 0, time.UTC)},
		{Hostname: "blog.acme.io", Port: 22, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:BlogLs0123456789NmLkJiHgFeDcBa9876543210ZyXwVuT", TrustedAt: time.Date(2023, 5, 5, 14, 0, 0, 0, time.UTC)},
	}
	for i := range fingerprints {
		if err := database.Create(&fingerprints[i]).Error; err != nil {
			fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
			os.Exit(1)
		}
	}

	snippets := []db.Snippet{
		{Name: "k8s · pods (ns)", Command: "kubectl get pods -n production -o wide", Tags: "k8s"},
		{Name: "docker · prune", Command: "docker system prune -af --volumes", Tags: "docker"},
		{Name: "logs · journal nginx", Command: "sudo journalctl -u nginx -f --since \"1 hour ago\"", Tags: "systemd"},
		{Name: "net · ss listeners", Command: "sudo ss -tlnp", Tags: "debug"},
		{Name: "db · psql local", Command: "psql \"postgresql://readonly@localhost:15432/app\" -c '\\dt'", Tags: "postgres"},
		{Name: "git · recent tags", Command: "git tag --sort=-creatordate | head -20", Tags: "git"},
	}
	for i := range snippets {
		if err := database.Create(&snippets[i]).Error; err != nil {
			fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
			os.Exit(1)
		}
	}

	hostIDByAlias := func(alias string) uint {
		var h db.Host
		if err := database.Where("alias = ?", alias).First(&h).Error; err != nil {
			fmt.Fprintf(os.Stderr, "gendemo: host %q: %v\n", alias, err)
			os.Exit(1)
		}
		return h.ID
	}

	forwards := []db.PortForward{
		{HostID: bastion.ID, LocalPort: 8443, RemoteHost: "kubernetes.default.svc", RemotePort: 443, Direction: "local"},
		{HostID: bastion.ID, LocalPort: 11080, RemoteHost: "localhost", RemotePort: 0, Direction: "dynamic"},
		{HostID: dbHost.ID, LocalPort: 15432, RemoteHost: "prod-app.cluster-abc.us-east-1.rds.amazonaws.com", RemotePort: 5432, Direction: "local"},
		{HostID: staging.ID, LocalPort: 18080, RemoteHost: "127.0.0.1", RemotePort: 8080, Direction: "local"},
		{HostID: appVPC.ID, LocalPort: 3000, RemoteHost: "127.0.0.1", RemotePort: 9090, Direction: "remote"},
		{HostID: hostIDByAlias("obs-grafana"), LocalPort: 13000, RemoteHost: "127.0.0.1", RemotePort: 3000, Direction: "local"},
		{HostID: hostIDByAlias("prod-redis"), LocalPort: 16379, RemoteHost: "127.0.0.1", RemotePort: 6379, Direction: "local"},
		{HostID: hostIDByAlias("stg-api"), LocalPort: 9080, RemoteHost: "127.0.0.1", RemotePort: 8080, Direction: "local"},
		{HostID: hostIDByAlias("gcp-bastion"), LocalPort: 16443, RemoteHost: "10.128.0.10", RemotePort: 443, Direction: "local"},
	}
	for i := range forwards {
		if err := database.Create(&forwards[i]).Error; err != nil {
			fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
			os.Exit(1)
		}
	}

	hist := []db.ConnectionHistory{
		{HostID: bastion.ID, ConnectedAt: time.Date(2026, 4, 10, 18, 12, 0, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 4, 10, 18, 45, 0, 0, time.Local)), Status: "success"},
		{HostID: stgBastion.ID, ConnectedAt: time.Date(2026, 4, 10, 10, 2, 0, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 4, 10, 10, 35, 0, 0, time.Local)), Status: "success"},
		{HostID: appVPC.ID, ConnectedAt: time.Date(2026, 4, 10, 9, 5, 10, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 4, 10, 9, 18, 0, 0, time.Local)), Status: "success"},
		{HostID: staging.ID, ConnectedAt: time.Date(2026, 4, 7, 11, 0, 0, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 4, 7, 11, 4, 30, 0, time.Local)), Status: "success"},
		{HostID: hetzner.ID, ConnectedAt: time.Date(2026, 4, 5, 2, 15, 0, 0, time.Local), Status: "error"},
		{HostID: homeLab.ID, ConnectedAt: time.Date(2026, 4, 8, 21, 40, 5, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 4, 8, 22, 1, 0, 0, time.Local)), Status: "success"},
		{HostID: hostIDByAlias("prod-web-01"), ConnectedAt: time.Date(2026, 4, 9, 16, 0, 0, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 4, 9, 16, 22, 0, 0, time.Local)), Status: "success"},
		{HostID: hostIDByAlias("stg-api"), ConnectedAt: time.Date(2026, 4, 6, 14, 30, 0, 0, time.Local), Status: "success"},
		{HostID: hostIDByAlias("gcp-bastion"), ConnectedAt: time.Date(2026, 4, 3, 8, 0, 0, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 4, 3, 8, 40, 0, 0, time.Local)), Status: "success"},
		{HostID: hostIDByAlias("oci-arm-runner"), ConnectedAt: time.Date(2026, 4, 4, 1, 0, 0, 0, time.Local), Status: "error"},
		{HostID: hostIDByAlias("dr-bastion"), ConnectedAt: time.Date(2026, 3, 28, 12, 0, 0, 0, time.Local), DisconnectedAt: ptrTime(time.Date(2026, 3, 28, 12, 5, 0, 0, time.Local)), Status: "success"},
	}
	for i := range hist {
		if err := database.Create(&hist[i]).Error; err != nil {
			fmt.Fprintf(os.Stderr, "gendemo: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("wrote %s\n", path)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
