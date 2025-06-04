package main

import (
	"log"
	"sshroulette/server"

	"golang.org/x/crypto/ssh"
)

func main() {
	config := &server.Config{
		KeyPath: "./id_rsa",
		Bind:    "0.0.0.0:2255",
		Pass:    "",
	}

	hostKey, err := server.LoadOrGenerateKey(config.KeyPath)
	if err != nil {
		log.Fatalf("Failed to load/generate host key: %v", err)
	}

	s := server.New(config, hostKey)

//	go s.BroadcastNotifications()

	sshConfig := &ssh.ServerConfig{
		NoClientAuth: config.Pass == "",
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if config.Pass == "" || string(password) == config.Pass {
				return nil, nil
			}
			return nil, err
		},
	}
	sshConfig.AddHostKey(hostKey)

	s.ListenAndServe(sshConfig)
}
