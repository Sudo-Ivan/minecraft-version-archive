package config

import (
	"fmt"
	"os"
)

func coordinatorAgentID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "host"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
