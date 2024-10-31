package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type VoterResult struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Votes int    `json:"votes"`
}

type CandidateResult struct {
	ID             uint          `json:"id"`
	Name           string        `json:"name"`
	VoteCount      int           `json:"vote_count"`
	VotePercentage float64       `json:"vote_percentage"` // New field for vote percentage
	Voters         []VoterResult `json:"voters" gorm:"-"`
}

// New structure for position results
type PositionResult struct {
	ID         uint              `json:"id"`
	Name       string            `json:"name"`
	Candidates []CandidateResult `json:"candidates" gorm:"-"` // Add gorm:"-" here
}

// New structure for category results
type CategoryResult struct {
	ID        uint             `json:"id"`
	Name      string           `json:"name"`
	Positions []PositionResult `json:"positions" gorm:"-"` // Add gorm:"-" here as well
}

// New Category model
type Category struct {
	gorm.Model
	Name      string
	Positions []Position
}

// Updated Position model to include CategoryID
type Position struct {
	gorm.Model
	Name       string
	CategoryID uint     // Foreign key for Category
	Category   Category // Belongs to Category
	Candidates []Candidate
}

// Rest of the existing models remain the same
type Candidate struct {
	gorm.Model
	Name       string
	PositionID uint
	Votes      []Vote `gorm:"foreignKey:CandidateID"`
}

type Voter struct {
	gorm.Model
	Name  string
	Phone string
	Votes []Vote
}

type CategoryResponse struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type PositionResponse struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	CategoryID uint   `json:"category_id"`
}

type CandidateResponse struct {
	ID         uint   `json:"id"`
	Name       string `json:"name"`
	PositionID uint   `json:"position_id"`
}

// New request structure for creating a category
type NewCategoryRequest struct {
	Name string `json:"name" binding:"required"`
}

// Updated request structure for creating a position
type NewPositionRequest struct {
	Name       string `json:"name" binding:"required"`
	CategoryID uint   `json:"category_id" binding:"required"`
}

// Rest of the request structures remain the same
type NewCandidateRequest struct {
	Name       string `json:"name" binding:"required"`
	PositionID uint   `json:"position_id" binding:"required"`
}

type VoteRequest struct {
	VoterName   string `json:"voter_name" binding:"required"`
	VoterPhone  string `json:"voter_phone" binding:"required"`
	CandidateID uint   `json:"candidate_id" binding:"required"`
	Amount      int    `json:"amount" binding:"required"`
}

type VotingSystem struct {
	DB *gorm.DB
}

// MpesaCallback represents the callback data structure from the payment provider
type MpesaCallback struct {
	TransactionStatus string `json:"transactionStatus"`
	TransactionReport string `json:"transactionReport"`
	Currency          string `json:"currency"`
	Amount            string `json:"amount"`
	NetAmount         string `json:"netAmount"`
	SecureId          string `json:"secureId"`
	ExternalId        string `json:"externalId"`
}

func NewVotingSystem(db *gorm.DB) *VotingSystem {
	return &VotingSystem{DB: db}
}

// Vote handles the voting process
// func (vs *VotingSystem) Vote(c *gin.Context) {
// 	var voteReq VoteRequest
// 	if err := c.ShouldBindJSON(&voteReq); err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
// 		return
// 	}

// 	err := vs.ProcessVote(voteReq.VoterName, voteReq.VoterPhone, voteReq.CandidateID)
// 	if err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
// 		return
// 	}

// 	c.JSON(http.StatusOK, gin.H{"message": "Vote recorded successfully"})
// }

// test
// test
// test
// test
// test
// test
// test

type Vote struct {
	gorm.Model
	VoterID     uint   `json:"voter_id"`
	CandidateID uint   `gorm:"index" json:"candidate_id"`
	ExternalID  string `gorm:"uniqueIndex;type:varchar(100);not null"`
	Status      string `json:"status"`
}

// SavePendingVote simplified to only use externalId
func (vs *VotingSystem) SavePendingVote(voterName, voterPhone string, candidateID uint, externalID string) error {
	var candidate Candidate
	if err := vs.DB.First(&candidate, candidateID).Error; err != nil {
		return errors.New("candidate not found")
	}

	voter, err := vs.GetOrCreateVoter(voterName, voterPhone)
	if err != nil {
		return fmt.Errorf("failed to retrieve or create voter: %w", err)
	}

	vote := Vote{
		VoterID:     voter.ID,
		CandidateID: candidateID,
		ExternalID:  externalID,
		Status:      "pending",
	}

	if err := vs.DB.Create(&vote).Error; err != nil {
		return err
	}

	return nil
}
func (vs *VotingSystem) MpesaCallbackHandler(c *gin.Context) {
	var callback MpesaCallback
	if err := c.ShouldBindJSON(&callback); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid callback data"})
		return
	}

	// Log the callback for debugging
	log.Printf("Received M-Pesa callback: %+v", callback)

	// Begin transaction
	tx := vs.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}

	// Find the pending vote
	var vote Vote
	if err := tx.Where("external_id = ? AND status = ?", callback.ExternalId, "pending").First(&vote).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "Pending vote not found"})
		return
	}

	// Check transaction status
	if callback.TransactionStatus == "COMPLETED" {
		vote.Status = "completed"
		// Save the updated vote
		if err := tx.Save(&vote).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update vote status"})
			return
		}
	} else {
		// Delete the vote instead of marking it as failed
		if err := tx.Delete(&vote).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete vote"})
			return
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Vote status updated to %s", vote.Status),
		"status":  vote.Status,
	})
}

func (vs *VotingSystem) Vote(c *gin.Context) {
	var voteReq VoteRequest
	if err := c.ShouldBindJSON(&voteReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Call MPESA payment function and get externalId
	externalID, err := vs.InitiateMpesaTransaction(voteReq.VoterName, voteReq.VoterPhone, voteReq.Amount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initiate MPESA transaction"})
		return
	}

	// Save vote with pending status
	err = vs.SavePendingVote(voteReq.VoterName, voteReq.VoterPhone, voteReq.CandidateID, externalID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save pending vote"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Vote recorded pending payment confirmation",
		"externalId": externalID,
	})
}

// InitiateMpesaTransaction initiates the transaction and returns an externalID
// InitiateMpesaTransaction initiates the transaction and returns an externalID
func (vs *VotingSystem) InitiateMpesaTransaction(voterName, voterPhone string, amount int) (string, error) {
	// Generate unique externalID
	externalID := fmt.Sprintf("TX_%d", time.Now().UnixNano())

	// Prepare MPESA transaction payload
	mpesaData := map[string]interface{}{
		"impalaMerchantId": "FEdkjwneifniwebfCO",
		"currency":         "KES",
		"amount":           amount,
		"payerPhone":       voterPhone,
		"mobileMoneySP":    "M-Pesa",
		"externalId":       externalID,
		"callbackUrl":      "https://5763-102-213-93-28.ngrok-free.app/mpesa-callback",
	}

	// Convert payload to JSON
	jsonData, err := json.Marshal(mpesaData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal MPESA request data: %w", err)
	}

	// Create request with headers
	req, err := http.NewRequest("POST", "https://official.mam-laka.com/api/?resource=merchant&action=initiate_mobile_payment", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create MPESA request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer ODhmNGY4Mjk5MTYzMDhiNWYxYmFjYTAyNzBiMzRhYjM=")
	req.Header.Set("Content-Type", "application/json")

	// Custom transport for HTTPS
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 30 * time.Second,
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send MPESA request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Log and confirm initiation
	log.Printf("MPESA initiation response: %s", string(respBody))
	return externalID, nil
}

func (vs *VotingSystem) GetOrCreateVoter(name, phone string) (*Voter, error) {
	// Directly create a new voter without checking for existing ones
	newVoter := Voter{Name: name, Phone: phone}
	if err := vs.DB.Create(&newVoter).Error; err != nil {
		return nil, errors.New("failed to create voter")
	}

	return &newVoter, nil
}

// GetVotersSummary retrieves a list of all voters who have voted, along with their vote count.
func (vs *VotingSystem) GetVotersSummary(c *gin.Context) {
	var voters []VoterResult

	// Query to get each voter's name, phone number, and count of "completed" votes
	err := vs.DB.Table("votes").
		Select("voters.name, voters.phone, COUNT(votes.id) AS votes").
		Joins("JOIN voters ON votes.voter_id = voters.id").
		Where("votes.status = ?", "completed").
		Group("voters.id").
		Order("votes DESC").
		Scan(&voters).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve voters"})
		return
	}

	// Calculate the total number of unique voters and "completed" votes
	var totalVoters int64
	var totalVotes int64
	vs.DB.Model(&Vote{}).Where("status = ?", "completed").Count(&totalVotes)
	vs.DB.Model(&Voter{}).
		Joins("JOIN votes ON voters.id = votes.voter_id").
		Where("votes.status = ?", "completed").
		Distinct().
		Count(&totalVoters)

	c.JSON(http.StatusOK, gin.H{
		"total_voters": totalVoters,
		"total_votes":  totalVotes,
		"voters":       voters,
	})
}

// ProcessVote processes a vote, checking for validity
func (vs *VotingSystem) ProcessVote(voterName, voterPhone string, candidateID uint) error {
	var candidate Candidate
	if err := vs.DB.First(&candidate, candidateID).Error; err != nil {
		return errors.New("candidate not found")
	}

	var voter Voter
	result := vs.DB.Where("name = ? AND phone = ?", voterName, voterPhone).First(&voter)
	if result.Error != nil {
		// Create new voter if not found
		voter = Voter{Name: voterName, Phone: voterPhone}
		if err := vs.DB.Create(&voter).Error; err != nil {
			return errors.New("failed to create voter")
		}
	}

	vote := Vote{
		VoterID:     voter.ID,
		CandidateID: candidateID,
	}

	if err := vs.DB.Create(&vote).Error; err != nil {
		return err
	}

	return nil
}

// Updated CreatePosition function to include category
func (vs *VotingSystem) CreatePosition(c *gin.Context) {
	var req NewPositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if category exists
	var category Category
	if err := vs.DB.First(&category, req.CategoryID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Category not found"})
		return
	}

	position := Position{Name: req.Name, CategoryID: req.CategoryID}
	if err := vs.DB.Create(&position).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create position"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Position created successfully", "position": position})
}

// New function to create a category
func (vs *VotingSystem) CreateCategory(c *gin.Context) {
	var req NewCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	category := Category{Name: req.Name}
	if err := vs.DB.Create(&category).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create category"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Category created successfully", "category": category})
}
func (vs *VotingSystem) DeleteCategory(c *gin.Context) {
	// Get the category ID from the URL parameter
	categoryID := c.Param("id")

	// Convert string ID to uint
	id, err := strconv.ParseUint(categoryID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	// Attempt to delete the category
	result := vs.DB.Delete(&Category{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete category"})
		return
	}

	// Check if a record was actually deleted
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted successfully"})
}

// CreateCandidate handles the creation of a new candidate
func (vs *VotingSystem) CreateCandidate(c *gin.Context) {
	var req NewCandidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	candidate := Candidate{Name: req.Name, PositionID: req.PositionID}
	if err := vs.DB.Create(&candidate).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create candidate"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Candidate created successfully", "candidate": candidate})
}

// Updated CheckCandidatesPosition to include category information
func (vs *VotingSystem) CheckCandidatesPosition(c *gin.Context) {
	var categories []Category
	var categoryResults []CategoryResult

	if err := vs.DB.Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve categories"})
		return
	}

	for _, category := range categories {
		categoryResult := CategoryResult{
			ID:   category.ID,
			Name: category.Name,
		}

		var positions []Position
		if err := vs.DB.Where("category_id = ?", category.ID).Find(&positions).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve positions"})
			return
		}

		for _, position := range positions {
			positionResult := PositionResult{
				ID:   position.ID,
				Name: position.Name,
			}

			// Count the total "completed" votes for the position
			var totalVotes int64
			if err := vs.DB.Model(&Vote{}).
				Joins("JOIN candidates ON candidates.id = votes.candidate_id").
				Where("candidates.position_id = ? AND votes.status = ?", position.ID, "completed").
				Count(&totalVotes).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count total votes"})
				return
			}

			// Get each candidate's "completed" vote count and calculate percentage
			var candidates []CandidateResult
			if err := vs.DB.Model(&Candidate{}).
				Select("candidates.id, candidates.name, COUNT(DISTINCT votes.voter_id) as vote_count").
				Joins("LEFT JOIN votes ON candidates.id = votes.candidate_id AND votes.status = ?", "completed").
				Where("candidates.position_id = ?", position.ID).
				Group("candidates.id").
				Order("vote_count DESC").
				Find(&candidates).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve candidates"})
				return
			}

			for i := range candidates {
				if totalVotes > 0 {
					candidates[i].VotePercentage = math.Round((float64(candidates[i].VoteCount) / float64(totalVotes)) * 100)
				} else {
					candidates[i].VotePercentage = 0
				}

				// Retrieve unique voters for each candidate with "completed" status votes
				var voters []VoterResult
				if err := vs.DB.Table("votes").
					Select("voters.name, voters.phone, COUNT(votes.id) as votes").
					Joins("JOIN voters ON votes.voter_id = voters.id").
					Where("votes.candidate_id = ? AND votes.status = ?", candidates[i].ID, "completed").
					Group("voters.id").
					Scan(&voters).Error; err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve voter details"})
					return
				}
				candidates[i].Voters = voters
			}

			positionResult.Candidates = candidates
			categoryResult.Positions = append(categoryResult.Positions, positionResult)
		}

		categoryResults = append(categoryResults, categoryResult)
	}

	c.JSON(http.StatusOK, categoryResults)
}

func (vs *VotingSystem) GetCategories(c *gin.Context) {
	var categories []CategoryResponse

	// Build the query with a CASE statement for custom ordering
	err := vs.DB.Model(&Category{}).
		Select("id, name").
		Order("CASE WHEN name = 'MATATU' THEN 0 ELSE 1 END, " + // Sort MATATU differently
			"CASE " +
			"WHEN name = 'MATATU' THEN id " + // Ascending for MATATU
			"ELSE -id " + // Descending for others (-id reverses the order)
			"END").
		Find(&categories).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve categories"})
		return
	}

	c.JSON(http.StatusOK, categories)
}

func (vs *VotingSystem) GetPositionsByCategory(c *gin.Context) {
	categoryID := c.Query("category_id")
	if categoryID == "" {
		var positions []struct {
			CategoryID   uint               `json:"category_id"`
			CategoryName string             `json:"category_name"`
			Positions    []PositionResponse `json:"positions"`
		}

		err := vs.DB.Model(&Position{}).
			Select("positions.category_id, categories.name as category_name").
			Joins("JOIN categories ON categories.id = positions.category_id").
			Group("positions.category_id").
			Find(&positions).Error

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve positions"})
			return
		}

		// For each category, get its positions
		for i := range positions {
			var categoryPositions []PositionResponse
			vs.DB.Model(&Position{}).
				Select("id, name, category_id").
				Where("category_id = ?", positions[i].CategoryID).
				Find(&categoryPositions)

			positions[i].Positions = categoryPositions
		}

		c.JSON(http.StatusOK, positions)
	} else {
		var positions []PositionResponse

		err := vs.DB.Model(&Position{}).
			Select("id, name, category_id").
			Where("category_id = ?", categoryID).
			Find(&positions).Error

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve positions"})
			return
		}

		c.JSON(http.StatusOK, positions)
	}
}

// GetCandidatesByPosition returns all candidates for a given position ID
func (vs *VotingSystem) GetCandidatesByPosition(c *gin.Context) {
	positionID := c.Query("position_id")
	if positionID == "" {
		// If no position ID is provided, return all candidates grouped by position
		var candidates []struct {
			PositionID   uint                `json:"position_id"`
			PositionName string              `json:"position_name"`
			CategoryName string              `json:"category_name"`
			Candidates   []CandidateResponse `json:"candidates"`
		}

		err := vs.DB.Model(&Candidate{}).
			Select("candidates.position_id, positions.name as position_name, categories.name as category_name").
			Joins("JOIN positions ON positions.id = candidates.position_id").
			Joins("JOIN categories ON categories.id = positions.category_id").
			Group("candidates.position_id").
			Find(&candidates).Error

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve candidates"})
			return
		}

		// For each position, get its candidates
		for i := range candidates {
			var positionCandidates []CandidateResponse
			vs.DB.Model(&Candidate{}).
				Select("id, name, position_id").
				Where("position_id = ?", candidates[i].PositionID).
				Find(&positionCandidates)

			candidates[i].Candidates = positionCandidates
		}

		c.JSON(http.StatusOK, candidates)
	} else {
		var candidates []CandidateResponse

		err := vs.DB.Model(&Candidate{}).
			Select("id, name, position_id").
			Where("position_id = ?", positionID).
			Find(&candidates).Error

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve candidates"})
			return
		}

		c.JSON(http.StatusOK, candidates)
	}
}

type Input struct {
	Amount int    `json:"amount" binding:"required"` // Amount to be sent in the transaction
	Phone  string `json:"phone" binding:"required"`  // Recipient's phone number in international format (e.g., "254712345678")
}

func mpesa(c *gin.Context) {
	var input Input

	// Bind JSON input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate input
	if input.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Amount must be greater than 0"})
		return
	}

	if !strings.HasPrefix(input.Phone, "254") || len(input.Phone) != 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Phone number must be in format 254XXXXXXXXX"})
		return
	}

	externalId := fmt.Sprintf("TX_%d", time.Now().UnixNano())

	// MPESA request payload
	mpesaData := map[string]interface{}{
		"impalaMerchantId": "FEdkjwneifniwebfCO",
		"currency":         "KES",
		"amount":           input.Amount,
		"payerPhone":       input.Phone,
		"mobileMoneySP":    "M-Pesa",
		"externalId":       externalId,
		"callbackUrl":      "https://9995-197-232-22-252.ngrok-free.app/mpesa-callback", // Update to your callback URL
	}

	// Convert mpesaData to JSON
	jsonData, err := json.Marshal(mpesaData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal MPESA request data"})
		return
	}

	// Create request with additional headers
	mpesaURL := "https://official.mam-laka.com/api/?resource=merchant&action=initiate_mobile_payment"
	req, err := http.NewRequest("POST", mpesaURL, bytes.NewBuffer(jsonData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create MPESA request"})
		return
	}

	// Set required headers
	req.Header.Set("Authorization", "Bearer ODhmNGY4Mjk5MTYzMDhiNWYxYmFjYTAyNzBiMzRhYjM=")
	req.Header.Set("Content-Type", "application/json")
	//req.Header.Set("Accept", "application/json")

	// Create custom transport to handle HTTPS
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Only use this in development
		},
	}

	// Create client with custom transport
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending MPESA request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send MPESA request", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	// Read and parse response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read response"})
		return
	}

	// Log the raw response for debugging
	log.Printf("Raw response: %s", string(respBody))

	// Notify the user that the transaction is initiated
	c.JSON(http.StatusOK, gin.H{
		"message":     "MPESA STK push initiated, waiting for callback",
		"transaction": externalId,
	})
}

// mpesaCallback function to handle the callback and update transaction status
func mpesaCallback(c *gin.Context) {
	var callbackData map[string]interface{}

	// Bind JSON callback data
	if err := c.ShouldBindJSON(&callbackData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid callback data"})
		return
	}

	// Process the callback data to retrieve the transaction status
	transactionId, _ := callbackData["externalId"].(string)
	status, _ := callbackData["status"].(string) // Assuming "status" field exists in callback

	// Respond based on the MPESA transaction status
	if status == "Success" {
		// Log success and notify user
		log.Printf("Transaction %s completed successfully", transactionId)
		c.JSON(http.StatusOK, gin.H{
			"message":       "MPESA transaction completed successfully",
			"transactionId": transactionId,
			"status":        status,
		})
	} else {
		// Log failure and notify user
		log.Printf("Transaction %s failed with status: %s", transactionId, status)
		c.JSON(http.StatusOK, gin.H{
			"message":       "MPESA transaction failed",
			"transactionId": transactionId,
			"status":        status,
		})
	}
}

func main() {
	// Database connection string
	dsn := "mamlakadev:@Mamlaka2021@tcp(localhost:3306)/fedco?charset=utf8mb4&parseTime=True&loc=Local"
	//dsn := "joelwasike:@Webuye2021@tcp(localhost:3306)/fedco?charset=utf8mb4&parseTime=True&loc=Local"

	// Connecting to the database
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Auto-migrate models
	db.AutoMigrate(&Category{}, &Position{}, &Candidate{}, &Voter{}, &Vote{})

	// Initialize the voting system with the database
	vs := NewVotingSystem(db)

	// Initialize the Gin router
	r := gin.Default()

	// CORS configuration
	config := cors.Config{
		AllowOrigins: []string{"http://fedco.mam-laka.com", "*mam-laka.com", "*"}, // Allow all origins
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},                    // Allowed HTTP methods (adjust as needed)
		AllowHeaders: []string{"Origin", "Content-Type", "Accept",
			"Authorization", "Access-Control-Allow-Origin"}, // Allowed request headers
		ExposeHeaders:    []string{"Content-Length"}, // Headers that can be exposed to the browser
		AllowCredentials: true,                       // Allows sending cookies and credentials like authorization tokens
		MaxAge:           12 * time.Hour,             // Cache preflight responses for 12 hours
	}

	// Apply CORS middleware to the router
	r.Use(cors.New(config))

	// Define the routes
	r.POST("/mpesa-callback", vs.MpesaCallbackHandler)
	r.POST("/mpesa", mpesa)
	r.POST("/createcategories", vs.CreateCategory)
	r.DELETE("/categories/:id", vs.DeleteCategory)
	r.POST("/createpositions", vs.CreatePosition)
	r.POST("/createcandidates", vs.CreateCandidate)
	r.POST("/vote", vs.Vote)
	r.GET("/checkcandidatesposition", vs.CheckCandidatesPosition)
	r.GET("/categories", vs.GetCategories)
	r.GET("/positions", vs.GetPositionsByCategory)
	r.GET("/candidates", vs.GetCandidatesByPosition)
	r.GET("/voters-summary", vs.GetVotersSummary)

	r.Run(":8081")
}

// type Vote struct {
//     gorm.Model
//     VoterID     uint   `gorm:"not null"`
//     CandidateID uint   `gorm:"index;not null"`
//     ExternalID  string `gorm:"uniqueIndex;type:varchar(100);not null"` // Explicit constraint definition
//     Status      string `gorm:"type:varchar(20);not null;default:'pending'"`
//     Voter       Voter     `gorm:"foreignKey:VoterID"`
//     Candidate   Candidate `gorm:"foreignKey:CandidateID"`
// }

// // SavePendingVote simplified to only use externalId
// func (vs *VotingSystem) SavePendingVote(voterName, voterPhone string, candidateID uint, externalID string) error {
// 	var candidate Candidate
// 	if err := vs.DB.First(&candidate, candidateID).Error; err != nil {
// 		return errors.New("candidate not found")
// 	}

// 	voter, err := vs.GetOrCreateVoter(voterName, voterPhone)
// 	if err != nil {
// 		return err
// 	}

// 	vote := Vote{
// 		VoterID:     voter.ID,
// 		CandidateID: candidateID,
// 		ExternalID:  externalID,
// 		Status:      "pending",
// 	}

// 	if err := vs.DB.Create(&vote).Error; err != nil {
// 		return err
// 	}

// 	return nil
// }
// func (vs *VotingSystem) MpesaCallbackHandler(c *gin.Context) {
// 	var callback MpesaCallback
// 	if err := c.ShouldBindJSON(&callback); err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid callback data"})
// 		return
// 	}

// 	// Log the callback for debugging
// 	log.Printf("Received M-Pesa callback: %+v", callback)

// 	// Begin transaction
// 	tx := vs.DB.Begin()
// 	if tx.Error != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
// 		return
// 	}

// 	// Find the pending vote
// 	var vote Vote
// 	if err := tx.Where("external_id = ? AND status = ?", callback.ExternalId, "pending").First(&vote).Error; err != nil {
// 		tx.Rollback()
// 		c.JSON(http.StatusNotFound, gin.H{"error": "Pending vote not found"})
// 		return
// 	}

// 	// Update vote based on transaction status
// 	if callback.TransactionStatus == "COMPLETED" {
// 		vote.Status = "completed"
// 	} else {
// 		vote.Status = "failed"
// 	}

// 	// Save the updated vote
// 	if err := tx.Save(&vote).Error; err != nil {
// 		tx.Rollback()
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update vote status"})
// 		return
// 	}

// 	// Commit transaction
// 	if err := tx.Commit().Error; err != nil {
// 		tx.Rollback()
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
// 		return
// 	}

// 	c.JSON(http.StatusOK, gin.H{
// 		"message": fmt.Sprintf("Vote status updated to %s", vote.Status),
// 		"status":  vote.Status,
// 	})
// }

// func (vs *VotingSystem) Vote(c *gin.Context) {
// 	var voteReq VoteRequest
// 	if err := c.ShouldBindJSON(&voteReq); err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
// 		return
// 	}

// 	// Call MPESA payment function and get externalId
// 	externalID, err := vs.InitiateMpesaTransaction(voteReq.VoterName, voteReq.VoterPhone)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initiate MPESA transaction"})
// 		return
// 	}

// 	// Save vote with pending status
// 	err = vs.SavePendingVote(voteReq.VoterName, voteReq.VoterPhone, voteReq.CandidateID, externalID)
// 	if err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save pending vote"})
// 		return
// 	}

// 	c.JSON(http.StatusOK, gin.H{
// 		"message":    "Vote recorded pending payment confirmation",
// 		"externalId": externalID,
// 	})
// }

// // InitiateMpesaTransaction initiates the transaction and returns an externalID
// // InitiateMpesaTransaction initiates the transaction and returns an externalID
// func (vs *VotingSystem) InitiateMpesaTransaction(voterName, voterPhone string) (string, error) {
// 	// Generate unique externalID
// 	externalID := fmt.Sprintf("TX_%d", time.Now().UnixNano())

// 	// Prepare MPESA transaction payload
// 	mpesaData := map[string]interface{}{
// 		"impalaMerchantId": "FEdkjwneifniwebfCO",
// 		"currency":         "KES",
// 		"amount":           10,
// 		"payerPhone":       voterPhone,
// 		"mobileMoneySP":    "M-Pesa",
// 		"externalId":       externalID,
// 		"callbackUrl":      "https://8eab-102-213-93-28.ngrok-free.app/mpesa-callback",
// 	}

// 	// Convert payload to JSON
// 	jsonData, err := json.Marshal(mpesaData)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to marshal MPESA request data: %w", err)
// 	}

// 	// Create request with headers
// 	req, err := http.NewRequest("POST", "https://official.mam-laka.com/api/?resource=merchant&action=initiate_mobile_payment", bytes.NewBuffer(jsonData))
// 	if err != nil {
// 		return "", fmt.Errorf("failed to create MPESA request: %w", err)
// 	}
// 	req.Header.Set("Authorization", "Bearer ODhmNGY4Mjk5MTYzMDhiNWYxYmFjYTAyNzBiMzRhYjM=")
// 	req.Header.Set("Content-Type", "application/json")

// 	// Custom transport for HTTPS
// 	client := &http.Client{
// 		Transport: &http.Transport{
// 			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
// 		},
// 		Timeout: 30 * time.Second,
// 	}

// 	// Send request
// 	resp, err := client.Do(req)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to send MPESA request: %w", err)
// 	}
// 	defer resp.Body.Close()

// 	// Read response
// 	respBody, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to read response: %w", err)
// 	}

// 	// Log and confirm initiation
// 	log.Printf("MPESA initiation response: %s", string(respBody))
// 	return externalID, nil
// }
