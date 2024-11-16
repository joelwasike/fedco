package models

import "gorm.io/gorm"

type Vote struct {
	gorm.Model
	VoterID     uint   `json:"voter_id"`
	CandidateID uint   `gorm:"index" json:"candidate_id"`
	ExternalID  string `gorm:"uniqueIndex;type:varchar(100);not null"`
	Status      string `json:"status"`
	Amount      int    `json:"amount"`
}
