package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"lexscriptsai-v3-backend/internal/config"
	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
)

type WhisperService struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewWhisperService(cfg *config.Config) *WhisperService {
	return &WhisperService{
		baseURL: cfg.WhisperAPIURL,
		token:   cfg.WhisperBaseToken,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

type whisperRequest struct {
	AudioURL          string `json:"audio_url"`
	EnableDiarization bool   `json:"enable_diarization"`
	EnableTranslation bool   `json:"enable_translation"`
	Language          string `json:"language"`
	TranscriptionMode string `json:"transcription_mode"`
}

type whisperResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
	Data    struct {
		TranscriptionID string `json:"transcription_id"`
		Status          string `json:"status"`
		Result          struct {
			Segments []whisperSegment `json:"segments"`
			Language string           `json:"language"`
		} `json:"result"`
	} `json:"data"`
	Message string `json:"message"`
}

type whisperSegment struct {
	Start   float64       `json:"start"`
	End     float64       `json:"end"`
	Text    string        `json:"text"`
	Speaker string        `json:"speaker"`
	Words   []whisperWord `json:"words"`
}

type whisperWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func (w *WhisperService) Submit(audioURL string, diarize bool, language string) (string, error) {
	if w.baseURL == "" {
		return "", fmt.Errorf("WHISPER_API_URL is not configured")
	}

	lang := language
	if lang == "" || lang == "auto" {
		lang = "en"
	}

	reqPayload := whisperRequest{
		AudioURL:          audioURL,
		EnableDiarization: diarize,
		EnableTranslation: false,
		Language:          lang,
		TranscriptionMode: "accurate",
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", w.baseURL+"/api/v1/transcribe", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("whisper API error status: %d", resp.StatusCode)
	}

	var parsed whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("failed to decode whisper response: %w", err)
	}

	jobID := parsed.Data.TranscriptionID
	if jobID == "" {
		return "", fmt.Errorf("whisper API returned empty transcription ID (msg: %s)", parsed.Message)
	}

	return jobID, nil
}

func (w *WhisperService) GetResult(externalID string) (*whisperResponse, error) {
	if externalID == "" {
		return nil, fmt.Errorf("external ID cannot be empty")
	}

	req, err := http.NewRequest("GET", w.baseURL+"/api/v1/transcribe/"+externalID+"/result", nil)
	if err != nil {
		return nil, err
	}

	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var bodyBytes []byte
		buf := new(bytes.Buffer)
		if _, readErr := buf.ReadFrom(resp.Body); readErr == nil {
			bodyBytes = buf.Bytes()
		}
		var parsed whisperResponse
		if err := json.Unmarshal(bodyBytes, &parsed); err == nil && parsed.Message != "" {
			return &parsed, nil
		}
		return nil, fmt.Errorf("whisper API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var parsed whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}

func (w *WhisperService) MapSegmentsToSpeakerBanks(segments []whisperSegment) (models.SpeakerBanks, int, int) {
	var speakerBanks models.SpeakerBanks
	totalWords := 0
	maxDuration := 0.0

	for _, seg := range segments {
		speakerName := strings.TrimSpace(seg.Speaker)
		if speakerName == "" {
			speakerName = "SPEAKER 01"
		}

		var words []models.Word
		if len(seg.Words) > 0 {
			for _, item := range seg.Words {
				words = append(words, models.Word{
					ID:        uuid.New().String(),
					Text:      item.Word,
					StartTime: item.Start,
					EndTime:   item.End,
				})
			}
		} else {
			rawWords := strings.Fields(seg.Text)
			count := len(rawWords)
			duration := seg.End - seg.Start
			step := 0.0
			if count > 0 {
				step = duration / float64(count)
			}
			for idx, txt := range rawWords {
				words = append(words, models.Word{
					ID:        uuid.New().String(),
					Text:      txt,
					StartTime: seg.Start + (float64(idx) * step),
					EndTime:   seg.Start + (float64(idx+1) * step),
				})
			}
		}

		totalWords += len(words)
		if seg.End > maxDuration {
			maxDuration = seg.End
		}

		// Merge consecutive segments belonging to the same speaker
		if len(speakerBanks) > 0 && strings.EqualFold(speakerBanks[len(speakerBanks)-1].Name, speakerName) {
			speakerBanks[len(speakerBanks)-1].Words = append(speakerBanks[len(speakerBanks)-1].Words, words...)
		} else {
			speakerBanks = append(speakerBanks, models.SpeakerBank{
				ID:             uuid.New().String(),
				Name:           speakerName,
				Words:          words,
				ParagraphIndex: len(speakerBanks),
			})
		}
	}

	log.Printf("[Whisper] Mapped %d segments into %d speaker banks, %d total words, duration: %.1fs", len(segments), len(speakerBanks), totalWords, maxDuration)
	return speakerBanks, int(maxDuration), totalWords
}
