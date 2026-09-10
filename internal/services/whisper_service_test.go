package services

import (
	"testing"
)

func TestMapSegmentsToSpeakerBanks_ContinuousSpeakerMerging(t *testing.T) {
	ws := &WhisperService{}

	segments := []whisperSegment{
		{
			Start:   0.0,
			End:     2.5,
			Text:    "Ladies and gentlemen, this is Gide Adeniran.",
			Speaker: "SPEAKER_00",
			Words: []whisperWord{
				{Word: "Ladies", Start: 0.0, End: 0.5},
				{Word: "and", Start: 0.5, End: 0.8},
				{Word: "gentlemen", Start: 0.8, End: 1.5},
			},
		},
		{
			Start:   3.0,
			End:     6.0,
			Text:    "I am here testing Lescript AI 3.0.",
			Speaker: "SPEAKER_00",
			Words: []whisperWord{
				{Word: "I", Start: 3.0, End: 3.2},
				{Word: "am", Start: 3.2, End: 3.4},
				{Word: "here", Start: 3.4, End: 3.8},
			},
		},
		{
			Start:   6.5,
			End:     9.0,
			Text:    "Thank you very much.",
			Speaker: "SPEAKER_01",
			Words: []whisperWord{
				{Word: "Thank", Start: 6.5, End: 7.0},
				{Word: "you", Start: 7.0, End: 7.5},
			},
		},
		{
			Start:   9.5,
			End:     12.0,
			Text:    "Let us continue the session.",
			Speaker: "SPEAKER_01",
			Words: []whisperWord{
				{Word: "Let", Start: 9.5, End: 9.8},
				{Word: "us", Start: 9.8, End: 10.0},
			},
		},
	}

	banks, duration, totalWords := ws.MapSegmentsToSpeakerBanks(segments)

	if len(banks) != 2 {
		t.Fatalf("Expected 2 speaker banks after merging continuous speaker turns, got %d", len(banks))
	}

	if banks[0].Name != "SPEAKER_00" {
		t.Errorf("Expected first bank speaker name SPEAKER_00, got %s", banks[0].Name)
	}

	if len(banks[0].Words) != 6 {
		t.Errorf("Expected 6 words in merged SPEAKER_00 bank, got %d", len(banks[0].Words))
	}

	if banks[1].Name != "SPEAKER_01" {
		t.Errorf("Expected second bank speaker name SPEAKER_01, got %s", banks[1].Name)
	}

	if len(banks[1].Words) != 4 {
		t.Errorf("Expected 4 words in merged SPEAKER_01 bank, got %d", len(banks[1].Words))
	}

	if duration != 12 {
		t.Errorf("Expected duration 12s, got %d", duration)
	}

	if totalWords != 10 {
		t.Errorf("Expected totalWords 10, got %d", totalWords)
	}
}

func TestMapSegmentsToSpeakerBanks_EmptySpeakerDefaulting(t *testing.T) {
	ws := &WhisperService{}

	segments := []whisperSegment{
		{Start: 0.0, End: 2.0, Text: "Hello world.", Speaker: ""},
		{Start: 2.5, End: 5.0, Text: "This is a non diarized test.", Speaker: ""},
	}

	banks, _, totalWords := ws.MapSegmentsToSpeakerBanks(segments)

	if len(banks) != 1 {
		t.Fatalf("Expected 1 speaker bank for un-diarized audio, got %d", len(banks))
	}

	if banks[0].Name != "SPEAKER 01" {
		t.Errorf("Expected default speaker name 'SPEAKER 01', got '%s'", banks[0].Name)
	}

	if totalWords != 8 {
		t.Errorf("Expected 8 words, got %d", totalWords)
	}
}
