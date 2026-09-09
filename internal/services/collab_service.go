package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"lexscriptsai-v3-backend/internal/auth"
	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  2048,
	WriteBufferSize: 2048,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		frontendURL := os.Getenv("FRONTEND_URL")
		if frontendURL == "" || os.Getenv("ENV") == "development" {
			return true
		}
		return strings.HasPrefix(origin, frontendURL) || strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1")
	},
}

type CollabMessage struct {
	Type          string      `json:"type"`
	TranscriptID  string      `json:"transcriptId,omitempty"`
	UserID        string      `json:"userId,omitempty"`
	UserName      string      `json:"userName,omitempty"`
	Data          interface{} `json:"data,omitempty"`
	Collaborating bool        `json:"collaborating,omitempty"`
	UserCount     int         `json:"userCount,omitempty"`
	Timestamp     int64       `json:"timestamp,omitempty"`
}

type CollabClient struct {
	service      *CollabService
	conn         *websocket.Conn
	send         chan []byte
	transcriptID string
	userID       string
	userName     string
	userEmail    string
	role         string
}

type UserPresence struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

type CollabService struct {
	db           *gorm.DB
	tokenService *auth.TokenService
	emailService        *EmailService
	notificationService *NotificationService
	rooms               map[string]map[*CollabClient]bool
	mu                  sync.RWMutex
}

func NewCollabService(db *gorm.DB, tokenService *auth.TokenService, emailService *EmailService, notificationService *NotificationService) *CollabService {
	return &CollabService{
		db:                  db,
		tokenService:        tokenService,
		emailService:        emailService,
		notificationService: notificationService,
		rooms:               make(map[string]map[*CollabClient]bool),
	}
}

func (s *CollabService) HandleWS(w http.ResponseWriter, r *http.Request, transcriptID string, userClaims *auth.JWTClaims) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("websocket upgrade failed: %w", err)
	}

	var dbUser models.User
	userName := userClaims.Email
	if err := s.db.First(&dbUser, "id = ?", userClaims.UserID).Error; err == nil && dbUser.Name != "" {
		userName = dbUser.Name
	}

	client := &CollabClient{
		service:      s,
		conn:         conn,
		send:         make(chan []byte, 256),
		transcriptID: transcriptID,
		userID:       userClaims.UserID.String(),
		userName:     userName,
		userEmail:    userClaims.Email,
		role:         string(userClaims.SystemRole),
	}

	if !s.registerClient(client) {
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Collab room reached maximum capacity"))
		conn.Close()
		return fmt.Errorf("collab room for transcript %s reached max capacity", transcriptID)
	}

	go client.writePump()
	go client.readPump()

	return nil
}

func (s *CollabService) registerClient(client *CollabClient) bool {
	s.mu.Lock()
	maxUsers := 10
	if envMax := os.Getenv("MAX_COLLABORATORS_PER_ROOM"); envMax != "" {
		if val, err := strconv.Atoi(envMax); err == nil && val > 0 {
			maxUsers = val
		}
	}

	if s.rooms[client.transcriptID] == nil {
		s.rooms[client.transcriptID] = make(map[*CollabClient]bool)
	}

	if len(s.rooms[client.transcriptID]) >= maxUsers {
		s.mu.Unlock()
		return false
	}

	s.rooms[client.transcriptID][client] = true
	s.mu.Unlock()

	s.broadcastPresence(client.transcriptID)
	return true
}

func (s *CollabService) unregisterClient(client *CollabClient) {
	s.mu.Lock()
	if room, exists := s.rooms[client.transcriptID]; exists {
		if _, ok := room[client]; ok {
			delete(room, client)
			close(client.send)
			if len(room) == 0 {
				delete(s.rooms, client.transcriptID)
			}
		}
	}
	s.mu.Unlock()

	s.broadcastPresence(client.transcriptID)
}

func (s *CollabService) broadcastPresence(transcriptID string) {
	s.mu.RLock()
	room := s.rooms[transcriptID]
	uniqueUsers := make(map[string]UserPresence)
	for c := range room {
		uniqueUsers[c.userID] = UserPresence{
			UserID:   c.userID,
			UserName: c.userName,
			Email:    c.userEmail,
			Role:     c.role,
		}
	}
	s.mu.RUnlock()

	var userList []UserPresence
	for _, u := range uniqueUsers {
		userList = append(userList, u)
	}

	distinctCount := len(uniqueUsers)
	// Collaboration mode only activates when >1 collaborator is active on that transcript
	isCollaborating := distinctCount > 1

	msg := CollabMessage{
		Type:          "PRESENCE_UPDATE",
		TranscriptID:  transcriptID,
		Collaborating: isCollaborating,
		UserCount:     distinctCount,
		Data:          userList,
		Timestamp:     time.Now().Unix(),
	}

	payload, _ := json.Marshal(msg)
	s.broadcastToRoom(transcriptID, payload, nil)
}

func (s *CollabService) broadcastToRoom(transcriptID string, payload []byte, sender *CollabClient) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if room, ok := s.rooms[transcriptID]; ok {
		for c := range room {
			if sender != nil && c == sender {
				continue
			}
			select {
			case c.send <- payload:
			default:
				log.Printf("[Collab] Client %s buffer full, dropping message", c.userID)
			}
		}
	}
}

func (c *CollabClient) readPump() {
	defer func() {
		c.service.unregisterClient(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(10 * 1024 * 1024) // 10MB
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[Collab] WebSocket read error: %v", err)
			}
			break
		}

		var incoming CollabMessage
		if err := json.Unmarshal(message, &incoming); err != nil {
			continue
		}

		incoming.UserID = c.userID
		incoming.UserName = c.userName
		incoming.TranscriptID = c.transcriptID
		incoming.Timestamp = time.Now().Unix()

		// Broadcast to other peers in room
		out, err := json.Marshal(incoming)
		if err == nil {
			c.service.broadcastToRoom(c.transcriptID, out, c)
		}

		if incoming.Type == "TRANSCRIPT_UPDATE" {
			go c.service.persistTranscriptContent(c.transcriptID, incoming.Data)
		}
	}
}

func (c *CollabClient) writePump() {
	ticker := time.NewTicker(25 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages if any
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ─── Sharing & Collaboration Access Management ────────────────────────────────

const MaxCollaboratorsLimit = 5

func (s *CollabService) ShareTranscript(transcriptID uuid.UUID, ownerID uuid.UUID, targetEmail string, role models.TranscriptShareRole) (*models.TranscriptShare, error) {
	var transcript models.Transcript
	if err := s.db.First(&transcript, "id = ?", transcriptID).Error; err != nil {
		return nil, errors.New("transcript not found")
	}

	if role == "" {
		role = models.ShareRoleEditor
	}

	// Verify target user exists
	var targetUser models.User
	if err := s.db.Where("LOWER(email) = ?", targetEmail).First(&targetUser).Error; err != nil {
		return nil, errors.New("user with this email does not exist on LexScriptsAI")
	}

	if targetUser.ID == ownerID {
		return nil, errors.New("you cannot share a transcript with yourself")
	}

	// Check collaborator limit
	var currentCount int64
	s.db.Model(&models.TranscriptShare{}).Where("transcript_id = ?", transcriptID).Count(&currentCount)
	if currentCount >= MaxCollaboratorsLimit {
		return nil, fmt.Errorf("maximum limit of %d collaborators reached for this transcript", MaxCollaboratorsLimit)
	}

	// Check if already shared
	var existing models.TranscriptShare
	if err := s.db.Where("transcript_id = ? AND shared_with_id = ?", transcriptID, targetUser.ID).First(&existing).Error; err == nil {
		existing.Role = role
		s.db.Save(&existing)
		return &existing, nil
	}

	share := models.TranscriptShare{
		TranscriptID: transcriptID,
		OwnerID:      ownerID,
		SharedWithID: targetUser.ID,
		UserEmail:    targetUser.Email,
		UserName:     targetUser.Name,
		Role:         role,
	}

	if err := s.db.Create(&share).Error; err != nil {
		return nil, fmt.Errorf("failed to save collaborator: %w", err)
	}

	// Dispatch In-App Notification
	var ownerUser models.User
	ownerName := "A colleague"
	if err := s.db.First(&ownerUser, "id = ?", ownerID).Error; err == nil && ownerUser.Name != "" {
		ownerName = ownerUser.Name
	}

	notif := models.Notification{
		AccountID:     targetUser.AccountID,
		UserID:        &targetUser.ID,
		Type:          "transcript_shared",
		Title:         "Transcript Shared With You",
		Message:       fmt.Sprintf("%s has shared '%s' with you as %s.", ownerName, transcript.Title, role),
		ActionURL:     fmt.Sprintf("/transcripts/%s", transcript.ID),
		RecipientRole: "user",
		ActorName:     ownerName,
		Read:          false,
	}
	if s.notificationService != nil {
		s.notificationService.CreateNotification(&notif)
	} else {
		s.db.Create(&notif)
	}

	// Send Email Notification if emailService configured
	if s.emailService != nil {
		go s.emailService.SendTranscriptShareEmail(targetUser.Email, targetUser.Name, ownerName, transcript.Title, transcript.ID.String(), string(role))
	}

	return &share, nil
}

func (s *CollabService) ListCollaborators(transcriptID uuid.UUID) ([]models.TranscriptShare, error) {
	var shares []models.TranscriptShare
	if err := s.db.Where("transcript_id = ?", transcriptID).Find(&shares).Error; err != nil {
		return nil, err
	}
	return shares, nil
}

func (s *CollabService) RemoveCollaborator(transcriptID uuid.UUID, ownerID uuid.UUID, targetUserID uuid.UUID) error {
	var transcript models.Transcript
	if err := s.db.First(&transcript, "id = ?", transcriptID).Error; err != nil {
		return errors.New("transcript not found")
	}

	if err := s.db.Where("transcript_id = ? AND shared_with_id = ?", transcriptID, targetUserID).Delete(&models.TranscriptShare{}).Error; err != nil {
		return err
	}

	return nil
}

type SharedTranscriptItem struct {
	Transcript models.Transcript `json:"transcript"`
	Role       string            `json:"role"`
	OwnerName  string            `json:"ownerName"`
	SharedAt   time.Time         `json:"sharedAt"`
}

func (s *CollabService) ListSharedWithUser(userID uuid.UUID) ([]SharedTranscriptItem, error) {
	var shares []models.TranscriptShare
	if err := s.db.Where("shared_with_id = ?", userID).Order("created_at DESC").Find(&shares).Error; err != nil {
		return nil, err
	}

	items := make([]SharedTranscriptItem, 0)
	for _, sh := range shares {
		var t models.Transcript
		if err := s.db.First(&t, "id = ? AND is_trashed = false", sh.TranscriptID).Error; err == nil {
			var owner models.User
			ownerName := "Unknown"
			if err := s.db.First(&owner, "id = ?", sh.OwnerID).Error; err == nil {
				ownerName = owner.Name
			}

			items = append(items, SharedTranscriptItem{
				Transcript: t,
				Role:       string(sh.Role),
				OwnerName:  ownerName,
				SharedAt:   sh.CreatedAt,
			})
		}
	}

	return items, nil
}

func (s *CollabService) persistTranscriptContent(transcriptID string, data interface{}) {
	if data == nil {
		return
	}
	tID, err := uuid.Parse(transcriptID)
	if err != nil {
		return
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	rawContent, ok := dataMap["content"].([]interface{})
	if !ok || len(rawContent) == 0 {
		return
	}

	var updatedBanks models.SpeakerBanks
	for si, secItem := range rawContent {
		secMap, ok := secItem.(map[string]interface{})
		if !ok {
			continue
		}
		speaker, _ := secMap["speaker"].(string)
		if speaker == "" {
			speaker = "SPEAKER"
		}

		var words []models.Word
		linesRaw, ok := secMap["lines"].([]interface{})
		if ok {
			for _, lineRaw := range linesRaw {
				lineStr, ok := lineRaw.(string)
				if ok {
					parts := strings.Fields(lineStr)
					for wi, p := range parts {
						words = append(words, models.Word{
							ID:        fmt.Sprintf("w-%d-%d", si, wi),
							Text:      p,
							StartTime: float64(wi) * 1.5,
							EndTime:   float64(wi)*1.5 + 1.2,
						})
					}
				}
			}
		}

		updatedBanks = append(updatedBanks, models.SpeakerBank{
			ID:             fmt.Sprintf("sb-%d", si),
			Name:           speaker,
			ParagraphIndex: si,
			Words:          words,
		})
	}

	if len(updatedBanks) > 0 {
		s.db.Model(&models.Transcript{}).
			Where("id = ?", tID).
			Updates(map[string]interface{}{
				"speaker_banks": updatedBanks,
				"updated_at":    time.Now(),
			})
	}
}
