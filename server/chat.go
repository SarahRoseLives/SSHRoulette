package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time" // For User.QueueTime (if still used, though it's less critical now)

	"golang.org/x/crypto/ssh"
)

// User struct (assuming it's defined in server/server.go or similar)
// For demonstration, defining it here if it's not global.
// In a real project, these structs would typically be in a separate, shared package or server.go.
/*
type User struct {
	Name       string
	Conn       ssh.Conn
	Ch         ssh.Channel
	Output     chan string         // Channel to send messages to the user
	Disconnect chan struct{}     // Channel to signal user disconnection
	inputBuf   []byte            // Buffer for user input
	QueueTime  time.Time         // Time user entered the queue
	LastPeer   *User             // To prevent immediate re-matching
}

// ChatPair struct (assuming it's defined in server/server.go or similar)
type ChatPair struct {
	User1 *User
	User2 *User
}

// Server struct (assuming it's defined in server/server.go or similar)
type Server struct {
	mu          sync.Mutex
	Users       map[string]*User
	Queue       []*User
	ActivePairs []*ChatPair
	// Add any other fields like HostSigners from your server.go
}

// generateRandomUsername - Assuming this function exists in server/server.go or needs to be added.
// For example:
func (s *Server) generateRandomUsername() string {
    // Basic example, consider a more robust unique ID generation in production
    return fmt.Sprintf("anon%04d", time.Now().Nanosecond()%10000)
}
*/

func (s *Server) HandleConnection(netConn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, config)
	if err != nil {
		log.Printf("Failed to handshake: %v", err)
		return
	}
	// Defer closing the main SSH connection
	defer func() {
		sshConn.Close()
		log.Printf("SSH connection from %s closed.", sshConn.RemoteAddr())
	}()

	go ssh.DiscardRequests(reqs)

	newChannel := <-chans
	if newChannel == nil || newChannel.ChannelType() != "session" {
		if newChannel != nil { // Ensure newChannel is not nil before calling Reject
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
		return
	}

	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("Could not accept channel: %v", err)
		return
	}

	user := &User{
		Name:       s.generateRandomUsername(),
		Conn:       sshConn,
		Ch:         channel,
		Output:     make(chan string, 100),
		Disconnect: make(chan struct{}), // Signaled when the user intends to disconnect or connection breaks
		inputBuf:   make([]byte, 0, 1024),
		QueueTime:  time.Now(), // Assign initial queue time
	}

	s.mu.Lock()
	s.Users[user.Name] = user
	s.mu.Unlock()

	log.Printf("New connection from %s (%s)", sshConn.RemoteAddr(), user.Name)

	// Send welcome and queue messages
	user.Output <- fmt.Sprintf("Welcome to SSH Roulette! Your name is %s\r\n", user.Name)
	user.Output <- "Waiting to be matched with another user...\r\n"

	// Start goroutines for user communication
	go s.acceptRequests(requests, user)
	go s.handleUserIO(user)
	go s.handleUserInput(user)

	// Add user to the queue for matching
	s.addToQueue(user)

	// Wait for the user to signal disconnection. This is the main cleanup point.
	<-user.Disconnect

	// --- User Disconnection Cleanup ---
	// This block runs when user.Disconnect is closed, meaning the user is truly leaving.

	log.Printf("Initiating cleanup for user %s.", user.Name)

	// Close the specific SSH channel associated with this session.
	// This will cause io.Read calls in handleUserInput and io.Write calls in handleUserIO to error out.
	// We'll close it after Output channel to ensure any final messages can be sent.
	user.Ch.Close()
	log.Printf("SSH channel for %s closed.", user.Name)


	// Close the output channel to stop the handleUserIO goroutine.
	// This should be done after sending any final messages.
	close(user.Output)
	log.Printf("User %s Output channel closed, handleUserIO should be exiting.", user.Name)

	// Remove the user from the global users map
	s.mu.Lock()
	delete(s.Users, user.Name)
	// Remove from queue if they are still there (e.g., disconnected while waiting)
	s.removeFromQueue(user)
	s.mu.Unlock()

	log.Printf("User %s fully disconnected", user.Name)
}

func (s *Server) acceptRequests(requests <-chan *ssh.Request, user *User) {
	for req := range requests {
		// Check if user is disconnecting before replying
		select {
		case <-user.Disconnect:
			log.Printf("User %s disconnecting, not accepting more requests.", user.Name)
			return
		default:
			// Continue
		}

		switch req.Type {
		case "shell", "pty-req", "window-change":
			req.Reply(true, nil)
			// Sending a newline here might cause issues if user.Output is already closed or closing.
			// It's generally better to rely on `handleUserInput` to echo newline for user input.
			// user.Output <- "\r\n"
		default:
			req.Reply(false, nil) // Reject unknown requests
		}
	}
	log.Printf("User %s acceptRequests goroutine exiting.", user.Name)
}

func (s *Server) handleUserIO(user *User) {
	for msg := range user.Output {
		_, err := io.WriteString(user.Ch, msg)
		if err != nil {
			log.Printf("Error writing to user %s channel: %v", user.Name, err)
			// On write error, assume connection is dead and signal disconnection
			// Ensure Disconnect channel is closed only once.
			select {
			case <-user.Disconnect:
				// Already closed, do nothing
			default:
				close(user.Disconnect) // Signal that the user has disconnected
			}
			break // Exit the goroutine
		}
	}
	log.Printf("User %s handleUserIO goroutine exiting.", user.Name)
}

func (s *Server) handleUserInput(user *User) {
	buf := make([]byte, 1024)

	for {
		// Check if user is disconnecting before attempting to read
		select {
		case <-user.Disconnect:
			log.Printf("User %s disconnecting, stopping input processing.", user.Name)
			return // Exit goroutine
		default:
			// Continue
		}

		n, err := user.Ch.Read(buf)
		if err != nil {
			log.Printf("Error reading from user %s channel: %v", user.Name, err)
			// On read error (like EOF), assume connection is dead and signal disconnection
			// Ensure Disconnect channel is closed only once.
			select {
			case <-user.Disconnect:
				// Already closed, do nothing
			default:
				close(user.Disconnect) // Signal disconnection
			}
			break // Exit the loop and goroutine
		}

		for _, b := range buf[:n] {
			// Check if user is disconnecting before processing each byte
			select {
			case <-user.Disconnect:
				return // Exit goroutine
			default:
				// Continue processing input
			}

			switch b {
			case 127: // Backspace
				if len(user.inputBuf) > 0 {
					user.inputBuf = user.inputBuf[:len(user.inputBuf)-1]
					user.Ch.Write([]byte("\b \b"))
				}
			case '\r', '\n': // Enter
				if len(user.inputBuf) > 0 {
					input := strings.TrimSpace(string(user.inputBuf))
					user.inputBuf = user.inputBuf[:0]

					switch input {
					case "/quit":
						s.handleUserCommandQuit(user)
						return // Exit this goroutine immediately after handling quit
					case "/next":
						s.handleUserCommandNext(user)
						// No return, user stays connected and waits for new match
					default:
						s.sendMessageToPair(user, input)
					}
				}
				user.Ch.Write([]byte("\r\n")) // Echo newline
			default:
				if b >= 32 && b <= 126 { // Printable ASCII characters
					user.inputBuf = append(user.inputBuf, b)
					user.Ch.Write([]byte{b}) // Echo character
				}
			}
		}
	}
	log.Printf("User %s handleUserInput goroutine exiting.", user.Name)
}

// handleUserCommandQuit is called when a user types /quit
func (s *Server) handleUserCommandQuit(user *User) {
	peer := s.findPeer(user)
	if peer != nil {
		peer.Output <- fmt.Sprintf("%s has left the chat. Searching for a new match...\r\n", user.Name)
		s.removePair(user)   // Remove the pair
		s.addToQueue(peer)   // Add the peer back to the queue
		peer.LastPeer = user // Set last peer for the peer
	}
	user.Output <- "Goodbye!\r\n"
	close(user.Disconnect) // Signal to HandleConnection that this user is going away
	// HandleConnection will then do the final cleanup of channels and maps
}

// handleUserCommandNext is called when a user types /next
func (s *Server) handleUserCommandNext(user *User) {
	peer := s.findPeer(user)
	if peer != nil {
		user.Output <- "Searching for a new match...\r\n"
		peer.Output <- fmt.Sprintf("%s has left the chat. Searching for a new match...\r\n", user.Name)

		// Store last peer information for both users to prevent immediate re-matching
		user.LastPeer = peer
		peer.LastPeer = user

		s.removePair(user) // Remove the active pair

		// Add both users back to the queue for a new match
		s.addToQueue(user)
		s.addToQueue(peer)
	} else {
		// If the user isn't in a pair (e.g., just joined and issued /next)
		user.Output <- "You are not currently in a chat. Searching for a new match...\r\n"
		s.addToQueue(user)
	}
}

func (s *Server) findPeer(user *User) *User {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pair := range s.ActivePairs {
		if pair.User1 == user {
			return pair.User2
		}
		if pair.User2 == user {
			return pair.User1
		}
	}
	return nil
}

func (s *Server) removePair(user *User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, pair := range s.ActivePairs {
		if pair.User1 == user || pair.User2 == user {
			// Efficiently remove the element from the slice
			s.ActivePairs = append(s.ActivePairs[:i], s.ActivePairs[i+1:]...)
			log.Printf("Removed pair involving %s from ActivePairs.", user.Name)
			return
		}
	}
}

func (s *Server) sendMessageToPair(sender *User, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	foundPair := false
	for _, pair := range s.ActivePairs {
		var receiver *User
		if pair.User1 == sender {
			receiver = pair.User2
		} else if pair.User2 == sender {
			receiver = pair.User1
		}

		if receiver != nil {
			foundPair = true
			// Safely send to receiver's output channel
			select {
			case receiver.Output <- fmt.Sprintf("[%s] %s\r\n", sender.Name, msg):
				// Message sent successfully
			default:
				// Receiver's channel is likely closed or full, indicating a stale connection.
				// Treat as if receiver disconnected.
				log.Printf("Attempted to send message to %s but their output channel is blocked or closed. Treating as disconnect.", receiver.Name)
				// Trigger receiver disconnection cleanup
				go func(p *User) {
					select {
					case <-p.Disconnect:
						// Already handled
					default:
						close(p.Disconnect) // Signal disconnection for the receiver
					}
				}(receiver)
				// Also remove the pair since one side is unresponsive
				s.removePair(sender) // This will remove the pair correctly regardless of who is sender/receiver
			}
			return // Message sent (or attempted and handled), exit
		}
	}

	// If no pair is found, inform the user they are not in a chat
	if !foundPair {
		sender.Output <- "You are not currently in a chat. Type /next to find someone!\r\n"
	}
}

// removeFromQueue safely removes a user from the queue.
// This function assumes s.mu is already locked by the caller.
func (s *Server) removeFromQueue(user *User) {
	for i, u := range s.Queue {
		if u == user {
			s.Queue = append(s.Queue[:i], s.Queue[i+1:]...)
			log.Printf("Removed %s from queue.", user.Name)
			return
		}
	}
}

func (s *Server) addToQueue(user *User) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prevent adding a user who is already in the process of disconnecting
	select {
	case <-user.Disconnect:
		log.Printf("Not adding %s to queue; user is disconnecting.", user.Name)
		return
	default:
		// Continue
	}

	// Check if user is already in queue to prevent duplicates
	for _, u := range s.Queue {
		if u == user {
			log.Printf("User %s already in queue.", user.Name)
			return // User already in queue
		}
	}

	s.Queue = append(s.Queue, user)
	log.Printf("Added %s to queue. Current queue size: %d", user.Name, len(s.Queue))

	// Trigger tryMatchUsers to run in a separate goroutine.
	go s.tryMatchUsers()
}

func (s *Server) tryMatchUsers() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only proceed if there are at least two users to potentially match
	if len(s.Queue) < 2 {
		return
	}

	newQueue := []*User{} // To build the queue for the next iteration
	matchedThisCycle := false
	matchedUsers := make(map[*User]bool) // To track users already processed/matched in this cycle

	// Iterate over a copy of the current queue to avoid issues with modification during iteration
	currentQueueCopy := make([]*User, len(s.Queue))
	copy(currentQueueCopy, s.Queue)

	// Clear the main queue to rebuild it with unmatched users
	s.Queue = []*User{}

	for i := 0; i < len(currentQueueCopy); i++ {
		u1 := currentQueueCopy[i]

		// Ensure u1 is not nil (shouldn't happen with proper queue management but safe to check)
		// and not already processed/matched in this cycle.
		if u1 == nil || matchedUsers[u1] {
			continue
		}

		// Check if u1 is still valid (not disconnected) before attempting any operations
		select {
		case <-u1.Disconnect:
			log.Printf("Skipping %s in tryMatchUsers (u1): disconnected.", u1.Name)
			matchedUsers[u1] = true // Mark as processed
			continue
		default:
			// User is still active, proceed
		}

		foundMatch := false
		for j := i + 1; j < len(currentQueueCopy); j++ {
			u2 := currentQueueCopy[j]

			// Ensure u2 is not nil and not already processed/matched
			if u2 == nil || matchedUsers[u2] {
				continue
			}

			// Check if u2 is still valid (not disconnected)
			select {
			case <-u2.Disconnect:
				log.Printf("Skipping %s in tryMatchUsers (u2): disconnected.", u2.Name)
				matchedUsers[u2] = true // Mark as processed
				continue
			default:
				// User is still active, proceed
			}

			// Prevent matching with the immediate last peer
			if u1.LastPeer == u2 || u2.LastPeer == u1 {
				continue
			}

			// --- Attempt to Match ---
			log.Printf("Attempting to pair %s with %s", u1.Name, u2.Name)

			// Try sending message to u1 first. Use a non-blocking send.
			u1Msg := fmt.Sprintf("Paired with %s! Type /next to find someone else or /quit to disconnect.\r\n", u2.Name)
			select {
			case u1.Output <- u1Msg:
				// Message sent to u1 successfully
			default:
				// u1's channel is likely closed or full. Treat u1 as disconnected.
				log.Printf("Failed to send match message to %s, output channel likely closed or full. Marking as disconnected.", u1.Name)
				select {
				case <-u1.Disconnect: // Already closed by HandleConnection or another goroutine
				default:
					close(u1.Disconnect) // Signal disconnection for u1
				}
				matchedUsers[u1] = true // Mark u1 as processed/disconnected
				foundMatch = false      // This match failed
				continue                // Move to the next potential u2 for current u1 (if any) or move to next u1
			}

			// If u1 is still active, try sending message to u2
			if !matchedUsers[u1] { // Check if u1 wasn't marked disconnected in the previous step
				u2Msg := fmt.Sprintf("Paired with %s! Type /next for another match or /quit to disconnect.\r\n", u1.Name)
				select {
				case u2.Output <- u2Msg:
					// Message sent to u2 successfully
					foundMatch = true // Both messages sent, match successful!
				default:
					// u2's channel is likely closed or full. Treat u2 as disconnected.
					log.Printf("Failed to send match message to %s, output channel likely closed or full. Marking as disconnected.", u2.Name)
					select {
					case <-u2.Disconnect: // Already closed
					default:
						close(u2.Disconnect) // Signal disconnection for u2
					}
					matchedUsers[u2] = true // Mark u2 as processed/disconnected

					// If u2 disconnected, u1 is left without a partner. Re-queue u1.
					log.Printf("Re-queueing %s as their potential match %s disconnected during pairing.", u1.Name, u2.Name)
					// No need to append to newQueue here directly, the outer loop's final
					// check handles it if !foundMatch and !matchedUsers[u1]
					foundMatch = false // This match failed
				}
			}

			if foundMatch {
				// Both users successfully sent messages, complete the pairing
				s.ActivePairs = append(s.ActivePairs, &ChatPair{User1: u1, User2: u2})
				matchedUsers[u1] = true // Mark both as matched for this cycle
				matchedUsers[u2] = true
				matchedThisCycle = true
				log.Printf("Successfully paired %s and %s", u1.Name, u2.Name)
				break // u1 found a match, move to the next u1 in the outer loop
			}
		}

		// If u1 was not successfully matched in this iteration and is still active (not marked as processed/disconnected)
		if !foundMatch && !matchedUsers[u1] {
			newQueue = append(newQueue, u1) // Add u1 to the new queue for the next matching attempt
		}
	}

	// Reconstruct the main queue from newQueue (which contains only unmatched, active users)
	s.Queue = newQueue
	log.Printf("tryMatchUsers finished. Remaining queue size: %d", len(s.Queue))

	// Re-run tryMatchUsers if matches were made and there are still enough users,
	// to ensure all possible matches are made in a batch. This helps in scenarios
	// where matches in the middle of the queue free up users for new matches.
	if matchedThisCycle && len(s.Queue) >= 2 {
		log.Println("More matches possible, re-running tryMatchUsers.")
		go s.tryMatchUsers()
	}
}
