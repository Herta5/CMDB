package collector

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHCollector discovers hosts via agentless SSH.
type SSHCollector struct{}

func init() {
	Register("ssh", &SSHCollector{})
}

func (c *SSHCollector) Name() string { return "ssh" }

func (c *SSHCollector) Collect(ctx context.Context, config map[string]interface{}) ([]CIDiscoveryData, error) {
	host, _ := config["host"].(string)
	port, ok := config["port"].(float64)
	if !ok {
		port = 22
	}
	user, _ := config["username"].(string)
	password, _ := config["password"].(string)
	keyFile, _ := config["key_file"].(string)

	if host == "" {
		return nil, fmt.Errorf("ssh: host is required")
	}

	authMethods := []ssh.AuthMethod{}
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}
	if keyFile != "" {
		key, err := ssh.ParsePrivateKey([]byte(keyFile))
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(key))
		}
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("ssh: no authentication method configured")
	}

	clientConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, int(port))
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("ssh: dial failed: %w", err)
	}
	defer client.Close()

	info := c.collectHostInfo(client, host)

	item := CIDiscoveryData{
		ExternalID: fmt.Sprintf("ssh-%s", host),
		CITypeName: "PhysicalServer",
		Name:       host,
		Attributes: info,
	}
	return []CIDiscoveryData{item}, nil
}

func (c *SSHCollector) collectHostInfo(client *ssh.Client, host string) map[string]interface{} {
	info := map[string]interface{}{
		"ip_address": host,
		"hostname":   host,
	}

	type cmdInfo struct {
		cmd  string
		key  string
		trim bool
	}

	commands := []cmdInfo{
		{"uname -s", "os", true},
		{"uname -r", "kernel_version", true},
		{"uname -m", "architecture", true},
		{"hostname", "hostname", true},
		{"cat /proc/cpuinfo | grep 'model name' | head -1 | cut -d: -f2", "cpu_model", true},
		{"nproc", "cpu_cores", true},
		{"free -b | grep Mem | awk '{print $2}'", "memory_bytes", true},
		{"df -B1 / | tail -1 | awk '{print $2}'", "disk_total_bytes", true},
	}

	for _, ci := range commands {
		out, err := runSSHCommand(client, ci.cmd)
		if err != nil {
			continue
		}
		if ci.trim {
			out = trimSpace(out)
		}
		info[ci.key] = out
	}

	return info
}

func runSSHCommand(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func trimSpace(s string) string {
	// remove leading/trailing whitespace and newlines
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	j := len(s) - 1
	for j >= i && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
		j--
	}
	return s[i : j+1]
}