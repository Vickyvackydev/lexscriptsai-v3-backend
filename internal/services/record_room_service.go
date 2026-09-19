package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type RecordRoomMessage struct {
	Type      string                 `json:"type"`
	Role      string                 `json:"role,omitempty"`
	RoomKey   string                 `json:"roomKey,omitempty"`
	Action    string                 `json:"action,omitempty"`
	State     map[string]interface{} `json:"state,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	PeerCount int                    `json:"peerCount,omitempty"`
	Timestamp int64                  `json:"timestamp,omitempty"`
	Message   string                 `json:"message,omitempty"`
}

type RecordRoomSession struct {
	RoomKey     string
	HostConn    *websocket.Conn
	RemoteConns map[*websocket.Conn]bool
	State       map[string]interface{}
	LastActive  time.Time
	mu          sync.RWMutex
}

type RecordRoomService struct {
	sessions map[string]*RecordRoomSession
	mu       sync.RWMutex
}

func NewRecordRoomService() *RecordRoomService {
	s := &RecordRoomService{
		sessions: make(map[string]*RecordRoomSession),
	}
	// Background cleanup of stale sessions older than 6 hours
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanupStaleSessions()
		}
	}()
	return s
}

func (s *RecordRoomService) cleanupStaleSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-6 * time.Hour)
	for key, sess := range s.sessions {
		sess.mu.RLock()
		inactive := sess.LastActive.Before(cutoff) && sess.HostConn == nil && len(sess.RemoteConns) == 0
		sess.mu.RUnlock()
		if inactive {
			delete(s.sessions, key)
		}
	}
}

func (s *RecordRoomService) getOrCreateSession(roomKey string) *RecordRoomSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, exists := s.sessions[roomKey]
	if !exists {
		sess = &RecordRoomSession{
			RoomKey:     roomKey,
			RemoteConns: make(map[*websocket.Conn]bool),
			State:       make(map[string]interface{}),
			LastActive:  time.Now(),
		}
		s.sessions[roomKey] = sess
	}
	return sess
}

func (s *RecordRoomService) GetRoomInfo(roomKey string) (map[string]interface{}, error) {
	s.mu.RLock()
	sess, exists := s.sessions[roomKey]
	s.mu.RUnlock()

	if !exists {
		return map[string]interface{}{
			"roomKey":   roomKey,
			"hasHost":   false,
			"peerCount": 0,
			"state":     nil,
		}, nil
	}

	sess.mu.RLock()
	defer sess.mu.RUnlock()

	return map[string]interface{}{
		"roomKey":   roomKey,
		"hasHost":   sess.HostConn != nil,
		"peerCount": len(sess.RemoteConns) + (map[bool]int{true: 1, false: 0}[sess.HostConn != nil]),
		"state":     sess.State,
	}, nil
}

func (s *RecordRoomService) DispatchCommand(roomKey string, action string, payload map[string]interface{}) error {
	s.mu.RLock()
	sess, exists := s.sessions[roomKey]
	s.mu.RUnlock()

	if !exists {
		return errors.New("record room session not found")
	}

	sess.mu.RLock()
	hostConn := sess.HostConn
	sess.mu.RUnlock()

	if hostConn == nil {
		return errors.New("no active host connected to this record room")
	}

	msg := RecordRoomMessage{
		Type:      "remote_command",
		RoomKey:   roomKey,
		Action:    action,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	bytes, _ := json.Marshal(msg)

	sess.mu.Lock()
	err := hostConn.WriteMessage(websocket.TextMessage, bytes)
	sess.LastActive = time.Now()
	sess.mu.Unlock()

	return err
}

func (s *RecordRoomService) HandleWS(w http.ResponseWriter, r *http.Request, roomKey string, role string) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("record room websocket upgrade failed: %w", err)
	}
	defer conn.Close()

	sess := s.getOrCreateSession(roomKey)

	isHost := role == "host"

	sess.mu.Lock()
	sess.LastActive = time.Now()
	if isHost {
		if sess.HostConn != nil {
			// Replace prior host connection if refreshed
			_ = sess.HostConn.Close()
		}
		sess.HostConn = conn
		log.Printf("[RecordRoom] Host connected to room: %s", roomKey)
	} else {
		sess.RemoteConns[conn] = true
		log.Printf("[RecordRoom] Remote peer connected to room: %s (total remotes: %d)", roomKey, len(sess.RemoteConns))
	}
	remoteCount := len(sess.RemoteConns)
	currentState := sess.State
	hostPresent := sess.HostConn != nil
	sess.mu.Unlock()

	// Notify current client of initial room status
	initMsg := RecordRoomMessage{
		Type:      "connected",
		Role:      role,
		RoomKey:   roomKey,
		State:     currentState,
		PeerCount: remoteCount + (map[bool]int{true: 1, false: 0}[hostPresent]),
		Timestamp: time.Now().UnixMilli(),
	}
	initBytes, _ := json.Marshal(initMsg)
	_ = conn.WriteMessage(websocket.TextMessage, initBytes)

	// Broadcast updated peer count to all clients in session
	s.broadcastPresence(sess)

	// Message loop
	for {
		_, msgBytes, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}

		var incoming RecordRoomMessage
		if unmarshalErr := json.Unmarshal(msgBytes, &incoming); unmarshalErr != nil {
			continue
		}

		sess.mu.Lock()
		sess.LastActive = time.Now()

		switch incoming.Type {
		case "state_update":
			// Host sends state update (elapsed, state, title, flags)
			if isHost {
				sess.State = incoming.State
				// Broadcast state to all remote companions
				outMsg := RecordRoomMessage{
					Type:      "room_state",
					RoomKey:   roomKey,
					State:     incoming.State,
					PeerCount: len(sess.RemoteConns) + 1,
					Timestamp: time.Now().UnixMilli(),
				}
				outBytes, _ := json.Marshal(outMsg)
				for rConn := range sess.RemoteConns {
					_ = rConn.WriteMessage(websocket.TextMessage, outBytes)
				}
			}

		case "command":
			// Remote companion triggers an action on the Host (start, pause, resume, stop, restart, flag)
			if sess.HostConn != nil {
				outMsg := RecordRoomMessage{
					Type:      "remote_command",
					RoomKey:   roomKey,
					Action:    incoming.Action,
					Payload:   incoming.Payload,
					Timestamp: time.Now().UnixMilli(),
				}
				outBytes, _ := json.Marshal(outMsg)
				_ = sess.HostConn.WriteMessage(websocket.TextMessage, outBytes)
			} else {
				errMsg := RecordRoomMessage{
					Type:    "error",
					Message: "Host recording device is currently offline",
				}
				errBytes, _ := json.Marshal(errMsg)
				_ = conn.WriteMessage(websocket.TextMessage, errBytes)
			}
		}

		sess.mu.Unlock()
	}

	// Disconnection cleanup
	sess.mu.Lock()
	if isHost {
		if sess.HostConn == conn {
			sess.HostConn = nil
			log.Printf("[RecordRoom] Host disconnected from room: %s", roomKey)
		}
	} else {
		delete(sess.RemoteConns, conn)
		log.Printf("[RecordRoom] Remote peer disconnected from room: %s (remaining: %d)", roomKey, len(sess.RemoteConns))
	}
	sess.mu.Unlock()

	s.broadcastPresence(sess)
	return nil
}

func (s *RecordRoomService) broadcastPresence(sess *RecordRoomSession) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()

	totalPeers := len(sess.RemoteConns)
	if sess.HostConn != nil {
		totalPeers += 1
	}

	msg := RecordRoomMessage{
		Type:      "peer_sync",
		RoomKey:   sess.RoomKey,
		PeerCount: totalPeers,
		State:     sess.State,
		Timestamp: time.Now().UnixMilli(),
	}
	bytes, _ := json.Marshal(msg)

	if sess.HostConn != nil {
		_ = sess.HostConn.WriteMessage(websocket.TextMessage, bytes)
	}
	for rConn := range sess.RemoteConns {
		_ = rConn.WriteMessage(websocket.TextMessage, bytes)
	}
}
