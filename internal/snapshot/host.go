package snapshot

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func NormalizeHost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, "/\\?#@") {
		return "", fmt.Errorf("invalid host %q", raw)
	}

	var host string
	if strings.HasPrefix(value, "[") {
		end := strings.IndexByte(value, ']')
		if end < 0 {
			return "", fmt.Errorf("invalid bracketed host %q", raw)
		}
		host = value[1:end]
		suffix := value[end+1:]
		if suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return "", fmt.Errorf("invalid host suffix %q", suffix)
			}
			if err := validatePort(suffix[1:]); err != nil {
				return "", err
			}
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return "", fmt.Errorf("bracketed host %q is not an IP address", raw)
		}
		return ip.String(), nil
	} else if ip := net.ParseIP(value); ip != nil {
		return ip.String(), nil
	} else if strings.Contains(value, ":") {
		if strings.Count(value, ":") != 1 {
			return "", fmt.Errorf("IPv6 host %q must be bracketed", raw)
		}
		var port string
		var err error
		host, port, err = net.SplitHostPort(value)
		if err != nil {
			return "", fmt.Errorf("invalid host %q: %w", raw, err)
		}
		if err = validatePort(port); err != nil {
			return "", err
		}
	} else {
		host = value
	}

	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", fmt.Errorf("invalid empty host %q", raw)
	}
	host = strings.ToLower(host)
	if len(host) > 253 {
		return "", fmt.Errorf("domain %q exceeds 253 bytes", raw)
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid domain label in %q", raw)
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", fmt.Errorf("domain %q must use ASCII hostname labels", raw)
			}
		}
	}
	return host, nil
}

func validatePort(port string) error {
	if port == "" {
		return fmt.Errorf("host port is empty")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("invalid host port %q", port)
	}
	return nil
}
