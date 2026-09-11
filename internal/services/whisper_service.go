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
	primaryURL    string
	primaryToken  string
	fallbackURL   string
	fallbackToken string
	client        *http.Client
}

func NewWhisperService(cfg *config.Config) *WhisperService {
	pURL := cfg.WhisperPrimaryAPIURL
	if pURL == "" {
		pURL = cfg.WhisperAPIURL
	}
	pToken := cfg.WhisperPrimaryToken
	if pToken == "" {
		pToken = cfg.WhisperBaseToken
	}

	return &WhisperService{
		primaryURL:    pURL,
		primaryToken:  pToken,
		fallbackURL:   cfg.WhisperFallbackAPIURL,
		fallbackToken: cfg.WhisperFallbackToken,
		client:        &http.Client{Timeout: 60 * time.Second},
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
	if w.primaryURL == "" && w.fallbackURL == "" {
		return "", fmt.Errorf("no Whisper API URL is configured")
	}

	// 1. Try Primary Whisper Service first
	if w.primaryURL != "" {
		log.Printf("[Whisper] Attempting job submission to Primary Whisper Service (%s)...", w.primaryURL)
		jobID, err := w.submitToEndpoint("Primary Whisper Service", w.primaryURL, w.primaryToken, audioURL, diarize, language)
		if err == nil {
			log.Printf("[Whisper] Successfully submitted job to Primary Whisper Service: %s", jobID)
			return "primary::" + jobID, nil
		}
		log.Printf("[Whisper] Primary Whisper Service submission failed (%v). Switching to Fallback Whisper Service...", err)
	}

	// 2. Fallback to Secondary Whisper Service
	if w.fallbackURL != "" {
		log.Printf("[Whisper] Attempting job submission to Fallback Whisper Service (%s)...", w.fallbackURL)
		jobID, err := w.submitToEndpoint("Fallback Whisper Service", w.fallbackURL, w.fallbackToken, audioURL, diarize, language)
		if err == nil {
			log.Printf("[Whisper] Successfully submitted job to Fallback Whisper Service: %s", jobID)
			return "fallback::" + jobID, nil
		}
		return "", fmt.Errorf("both Primary and Fallback Whisper services failed. Fallback error: %w", err)
	}

	return "", fmt.Errorf("whisper service unavailable")
}

func (w *WhisperService) submitToEndpoint(serviceName, baseURL, token, audioURL string, diarize bool, language string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("%s URL is not configured", serviceName)
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

	req, err := http.NewRequest("POST", baseURL+"/api/v1/transcribe", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s request failed: %w", serviceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s error status: %d", serviceName, resp.StatusCode)
	}

	var parsed whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("%s decode error: %w", serviceName, err)
	}

	jobID := parsed.Data.TranscriptionID
	if jobID == "" {
		return "", fmt.Errorf("%s returned empty transcription ID (msg: %s)", serviceName, parsed.Message)
	}

	return jobID, nil
}

func (w *WhisperService) GetResult(externalID string) (*whisperResponse, error) {
	if externalID == "" {
		return nil, fmt.Errorf("external ID cannot be empty")
	}

	if strings.HasPrefix(externalID, "primary::") {
		realID := strings.TrimPrefix(externalID, "primary::")
		res, err := w.getResultFromEndpoint("Primary Whisper Service", w.primaryURL, w.primaryToken, realID)
		if err == nil {
			return res, nil
		}
		log.Printf("[Whisper] GetResult from Primary Service failed (%v). Querying Fallback Service...", err)
		if w.fallbackURL != "" {
			return w.getResultFromEndpoint("Fallback Whisper Service", w.fallbackURL, w.fallbackToken, realID)
		}
		return nil, err
	}

	if strings.HasPrefix(externalID, "fallback::") {
		realID := strings.TrimPrefix(externalID, "fallback::")
		return w.getResultFromEndpoint("Fallback Whisper Service", w.fallbackURL, w.fallbackToken, realID)
	}

	// Legacy externalID without prefix
	res, err := w.getResultFromEndpoint("Primary Whisper Service", w.primaryURL, w.primaryToken, externalID)
	if err == nil {
		return res, nil
	}
	if w.fallbackURL != "" {
		log.Printf("[Whisper] Legacy GetResult from Primary failed (%v), trying Fallback Service...", err)
		return w.getResultFromEndpoint("Fallback Whisper Service", w.fallbackURL, w.fallbackToken, externalID)
	}

	return nil, err
}

func (w *WhisperService) getResultFromEndpoint(serviceName, baseURL, token, externalID string) (*whisperResponse, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("%s URL is not configured", serviceName)
	}

	req, err := http.NewRequest("GET", baseURL+"/api/v1/transcribe/"+externalID+"/result", nil)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
		return nil, fmt.Errorf("%s returned status %d: %s", serviceName, resp.StatusCode, string(bodyBytes))
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
