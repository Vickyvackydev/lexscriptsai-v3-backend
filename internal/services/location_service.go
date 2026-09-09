package services

import (
	"errors"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LocationService struct {
	db *gorm.DB
}

func NewLocationService(db *gorm.DB) *LocationService {
	return &LocationService{db: db}
}

func (s *LocationService) ListLocations() ([]models.Location, error) {
	var locations []models.Location
	if err := s.db.Where("is_active = ?", true).Order("created_at DESC").Find(&locations).Error; err != nil {
		return nil, err
	}
	return locations, nil
}

type CreateLocationInput struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

func (s *LocationService) CreateLocation(input CreateLocationInput) (*models.Location, error) {
	if input.Name == "" {
		return nil, errors.New("location name is required")
	}

	location := models.Location{
		Name:     input.Name,
		State:    input.State,
		IsActive: true,
	}

	if err := s.db.Create(&location).Error; err != nil {
		return nil, err
	}
	return &location, nil
}

func (s *LocationService) UpdateLocation(id uuid.UUID, name string, state string) (*models.Location, error) {
	var loc models.Location
	if err := s.db.Where("id = ?", id).First(&loc).Error; err != nil {
		return nil, err
	}

	loc.Name = name
	loc.State = state
	if err := s.db.Save(&loc).Error; err != nil {
		return nil, err
	}
	return &loc, nil
}

func (s *LocationService) DeleteLocation(id uuid.UUID) error {
	var loc models.Location
	if err := s.db.Where("id = ?", id).First(&loc).Error; err != nil {
		return err
	}

	loc.IsActive = false
	if err := s.db.Save(&loc).Error; err != nil {
		return err
	}
	return s.db.Delete(&loc).Error
}
