package handlers

import (
	"errors"
	"fedco/models"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"regexp"
	"strconv"
	"strings"
)

// StripData extracts data from the given text and returns it as a map
func StripData(text string) (map[string]interface{}, error) {
	// Extract the part inside curly braces
	startIndex := strings.Index(text, "{")
	if startIndex == -1 {
		return nil, fmt.Errorf("invalid input format")
	}

	// Get the substring from the first '{' to the last '}'
	rawData := text[startIndex:]
	endIndex := strings.LastIndex(rawData, "}")
	if endIndex == -1 {
		return nil, fmt.Errorf("missing closing brace")
	}
	rawData = rawData[:endIndex+1]

	// Use regex to capture key-value pairs more robustly, allowing underscores in values
	regex := regexp.MustCompile(`(\w+):([a-zA-Z0-9-_]+)`)
	matches := regex.FindAllStringSubmatch(rawData, -1)

	// Convert matches to a map
	result := make(map[string]interface{})
	for _, match := range matches {
		key := match[1]
		value := match[2]

		// Check if the key is Amount or NetAmount and convert them to an integer
		if key == "Amount" || key == "NetAmount" {
			// Convert value to integer if it's a number
			amount, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid amount format: %v", err)
			}
			result[key] = amount
		} else {
			// Keep other fields as string
			result[key] = value
		}
	}

	fmt.Println(result)

	return result, nil
}

// UpdateTransactionAmount checks if a record exists with the given ExternalId and updates the Amount field.
func UpdateTransactionAmount(db *gorm.DB, data map[string]interface{}) error {
	// Check for ExternalId in the data map
	externalId, ok := data["ExternalId"].(string)
	if !ok {
		return fmt.Errorf("ExternalId is missing or not a string")
	}

	fmt.Println(externalId)

	// Check for Amount in the data map and ensure it's an integer
	newAmount, ok := data["Amount"].(int)
	if !ok {
		return fmt.Errorf("Amount is missing or not an integer")
	}

	// Check if transaction exists
	var vote models.Vote
	result := db.Where("external_id = ?", externalId).First(&vote)

	fmt.Println(result)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return fmt.Errorf("vote with ExternalId %s not found", externalId)
		}
		return result.Error
	}

	// Update the Amount field
	vote.Amount = newAmount
	updateResult := db.Model(&vote).Clauses(clause.Returning{}).Updates(vote)
	if updateResult.Error != nil {
		return fmt.Errorf("failed to update amount: %v", updateResult.Error)
	}

	return nil
}

// POST request to update transaction based on the provided text

func UpdateVoteHandler(db *gorm.DB, c *gin.Context) {
	var requestBody struct {
		Text string `json:"text"`
	}

	// Bind the JSON body to the requestBody struct
	if err := c.ShouldBindJSON(&requestBody); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request body"})
		return
	}

	// Extract data from the provided text
	data, err := StripData(requestBody.Text)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Update the database
	err = UpdateTransactionAmount(db, data)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "Transaction amount updated successfully"})
}
