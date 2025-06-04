package server

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Config struct {
	KeyPath string
	Bind    string
	Pass    string
}

type User struct {
	Name       string
	Conn       *ssh.ServerConn
	Ch         ssh.Channel
	LastPeer   *User
	QueueTime  time.Time
	Output     chan string
	Disconnect chan struct{}
	inputBuf   []byte
}

type ChatPair struct {
	User1 *User
	User2 *User
}

type Server struct {
	Config      *Config
	HostKey     ssh.Signer
	Users       map[string]*User
	Queue       []*User
	ActivePairs []*ChatPair
	mu          sync.Mutex
	NotifyAll   chan string
	Shutdown    chan struct{}
}

func New(cfg *Config, hostKey ssh.Signer) *Server {
	return &Server{
		Config:      cfg,
		HostKey:     hostKey,
		Users:       make(map[string]*User),
		Queue:       make([]*User, 0),
		ActivePairs: make([]*ChatPair, 0),
		NotifyAll:   make(chan string, 100),
		Shutdown:    make(chan struct{}),
	}
}

func (s *Server) ListenAndServe(sshConfig *ssh.ServerConfig) {
	listener, err := net.Listen("tcp", s.Config.Bind)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", s.Config.Bind, err)
	}
	defer listener.Close()

	log.Printf("SSH roulette server listening on %s", s.Config.Bind)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept incoming connection: %v", err)
			continue
		}
		go s.HandleConnection(conn, sshConfig)
	}
}

func (s *Server) generateRandomUsername() string {
	max := big.NewInt(9999)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		n = big.NewInt(0)
	}
	return fmt.Sprintf("anon%04d", n)
}
