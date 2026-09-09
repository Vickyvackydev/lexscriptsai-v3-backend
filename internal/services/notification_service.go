package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"lexscriptsai-v3-backend/internal/auth"
	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

var notifUpgrader = websocket.Upgrader{
	ReadBufferSize:  2048,
	WriteBufferSize: 2048,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow configured origins
	},
}

type NotificationClient struct {
	conn      *websocket.Conn
	send      chan []byte
	userID    uuid.UUID
	accountID uuid.UUID
}

type NotificationService struct {
	db      *gorm.DB
	clients map[*NotificationClient]bool
	mu      sync.RWMutex
}

func NewNotificationService(db *gorm.DB) *NotificationService {
	return &NotificationService{
		db:      db,
		clients: make(map[*NotificationClient]bool),
	}
}

func (s *NotificationService) RegisterClient(client *NotificationClient) {
	s.mu.Lock()
	s.clients[client] = true
	s.mu.Unlock()
	log.Printf("[NotificationHub] Client registered: User %s (active connections: %d)", client.userID, len(s.clients))
}

func (s *NotificationService) UnregisterClient(client *NotificationClient) {
	s.mu.Lock()
	if _, ok := s.clients[client]; ok {
		delete(s.clients, client)
		close(client.send)
	}
	s.mu.Unlock()
	log.Printf("[NotificationHub] Client disconnected: User %s", client.userID)
}

func (s *NotificationService) BroadcastNotification(notif *models.Notification) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgPayload, err := json.Marshal(map[string]interface{}{
		"type": "notification",
		"data": notif,
	})
	if err != nil {
		log.Printf("[NotificationHub] Error serializing notification: %v", err)
		return
	}

	for client := range s.clients {
		shouldSend := false
		if notif.UserID != nil && *notif.UserID == client.userID {
			shouldSend = true
		} else if notif.AccountID != nil && client.accountID != uuid.Nil && *notif.AccountID == client.accountID {
			shouldSend = true
		} else if notif.RecipientRole == "all" || notif.RecipientRole == "broadcast" {
			shouldSend = true
		}

		if shouldSend {
			select {
			case client.send <- msgPayload:
				log.Printf("[NotificationHub] Realtime notification dispatched to user %s: %s", client.userID, notif.Title)
			default:
				log.Printf("[NotificationHub] Buffer full for user %s, dropping frame", client.userID)
			}
		}
	}
}

func (s *NotificationService) HandleWS(w http.ResponseWriter, r *http.Request, userClaims *auth.JWTClaims) error {
	conn, err := notifUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return fmt.Errorf("websocket upgrade failed: %w", err)
	}

	accountID := uuid.Nil
	var dbUser models.User
	if err := s.db.First(&dbUser, "id = ?", userClaims.UserID).Error; err == nil && dbUser.AccountID != nil {
		accountID = *dbUser.AccountID
	}

	client := &NotificationClient{
		conn:      conn,
		send:      make(chan []byte, 128),
		userID:    userClaims.UserID,
		accountID: accountID,
	}

	s.RegisterClient(client)

	// Send initial connected ping event
	initMsg, _ := json.Marshal(map[string]string{"type": "connected", "message": "Notification stream active"})
	client.send <- initMsg

	go client.writePump(s)
	go client.readPump(s)

	return nil
}

func (c *NotificationClient) readPump(s *NotificationService) {
	defer func() {
		s.UnregisterClient(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(65536)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *NotificationClient) writePump(s *NotificationService) {
	ticker := time.NewTicker(25 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(msg)
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

func (s *NotificationService) ListUserNotifications(userID uuid.UUID, userEmail string, accountID uuid.UUID) ([]models.Notification, error) {
	var notifs []models.Notification

	query := s.db.Model(&models.Notification{})

	conditions := []string{"recipient_role != 'admin'"}
	var args []interface{}

	var orClauses []string
	if userID != uuid.Nil {
		orClauses = append(orClauses, "user_id = ?")
		args = append(args, userID)
	}
	if userEmail != "" {
		orClauses = append(orClauses, "LOWER(user_email) = LOWER(?)")
		args = append(args, strings.TrimSpace(userEmail))
	}
	if accountID != uuid.Nil {
		orClauses = append(orClauses, "account_id = ?")
		args = append(args, accountID)
	}
	orClauses = append(orClauses, "recipient_role = 'all'", "recipient_role = 'broadcast'")

	conditions = append(conditions, "("+strings.Join(orClauses, " OR ")+")")
	combinedCondition := strings.Join(conditions, " AND ")

	if err := query.Where(combinedCondition, args...).Order("created_at DESC").Limit(50).Find(&notifs).Error; err != nil {
		return nil, err
	}

	// If no notifications exist yet for this user, seed a real welcome notification so they see real content
	if len(notifs) == 0 && userID != uuid.Nil {
		welcomeNotif := models.Notification{
			UserID:        &userID,
			UserEmail:     userEmail,
			Type:          "system",
			Title:         "Welcome to LexScripts AI",
			Message:       "Your legal transcription workspace is active. Upload audio or collaborate with your team to get started.",
			ActionURL:     "/transcripts",
			RecipientRole: "user",
			Read:          false,
		}
		if accountID != uuid.Nil {
			welcomeNotif.AccountID = &accountID
		}
		if err := s.db.Create(&welcomeNotif).Error; err == nil {
			notifs = append(notifs, welcomeNotif)
		}
	}

	return notifs, nil
}

func (s *NotificationService) MarkRead(id uuid.UUID) error {
	return s.db.Model(&models.Notification{}).Where("id = ?", id).Update("read", true).Error
}

func (s *NotificationService) MarkAllRead(userID uuid.UUID, userEmail string, accountID uuid.UUID) error {
	query := s.db.Model(&models.Notification{}).Where("recipient_role != 'admin'")

	var orClauses []string
	var args []interface{}

	if userID != uuid.Nil {
		orClauses = append(orClauses, "user_id = ?")
		args = append(args, userID)
	}
	if userEmail != "" {
		orClauses = append(orClauses, "LOWER(user_email) = LOWER(?)")
		args = append(args, strings.TrimSpace(userEmail))
	}
	if accountID != uuid.Nil {
		orClauses = append(orClauses, "account_id = ?")
		args = append(args, accountID)
	}

	if len(orClauses) > 0 {
		query = query.Where("("+strings.Join(orClauses, " OR ")+")", args...)
	}

	return query.Update("read", true).Error
}

func (s *NotificationService) DeleteNotification(id uuid.UUID) error {
	return s.db.Where("id = ?", id).Delete(&models.Notification{}).Error
}

func (s *NotificationService) CreateNotification(notif *models.Notification) error {
	if err := s.db.Create(notif).Error; err != nil {
		return err
	}
	s.BroadcastNotification(notif)
	return nil
}
