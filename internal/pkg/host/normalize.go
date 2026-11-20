package host

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func Normalize(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, "/\\?#@") {
		return "", fmt.Errorf("invalid host %q", raw)
	}

	var hostname string
	if strings.HasPrefix(value, "[") {
		end := strings.IndexByte(value, ']')
		if end < 0 {
			return "", fmt.Errorf("invalid bracketed host %q", raw)
		}
		hostname = value[1:end]
		suffix := value[end+1:]
		if suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return "", fmt.Errorf("invalid host suffix %q", suffix)
			}
			if err := validatePort(suffix[1:]); err != nil {
				return "", err
			}
		}
		ip := net.ParseIP(hostname)
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
		hostname, port, err = net.SplitHostPort(value)
		if err != nil {
			return "", fmt.Errorf("invalid host %q: %w", raw, err)
		}
		if err = validatePort(port); err != nil {
			return "", err
		}
	} else {
		hostname = value
	}

	if ip := net.ParseIP(hostname); ip != nil {
		return ip.String(), nil
	}
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" {
		return "", fmt.Errorf("invalid empty host %q", raw)
	}
	hostname = strings.ToLower(hostname)
	if len(hostname) > 253 {
		return "", fmt.Errorf("domain %q exceeds 253 bytes", raw)
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid domain label in %q", raw)
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", fmt.Errorf("domain %q must use ASCII hostname labels", raw)
			}
		}
	}
	return hostname, nil
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
