package controller

import (
	"encoding/json"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/autoroute"
	"github.com/gin-gonic/gin"
)

// maxClassifyTextBytes bounds the text an admin can submit for a trial rating.
const maxClassifyTextBytes = 64 << 10

type classifyDimension struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
	Signal string  `json:"signal"`
}

// ClassifyAutoModelText rates a piece of text the way the auto router would
// and says why, so an admin can tune keywords and boundaries against real
// requests without sending traffic. "classifier" carries the overrides as the
// settings page holds them, saved or not (empty means the defaults); it is
// validated exactly as a saved config is.
func ClassifyAutoModelText(c *gin.Context) {
	var request struct {
		Text       string          `json:"text"`
		HasTools   bool            `json:"has_tools"`
		HasImages  bool            `json:"has_images"`
		Classifier json.RawMessage `json:"classifier"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if len(request.Text) > maxClassifyTextBytes {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "text is too long"})
		return
	}
	cfg, err := autoroute.ParseConfigOverlay(request.Classifier)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	classifier, err := autoroute.New(cfg)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	result := classifier.Classify(autoroute.Input{Text: request.Text, HasTools: request.HasTools, HasImages: request.HasImages})

	dimensions := make([]classifyDimension, 0, len(result.Dimensions))
	for _, d := range result.Dimensions {
		dimensions = append(dimensions, classifyDimension{Name: d.Name, Score: d.Score, Weight: d.Weight, Signal: d.Signal})
	}
	signals := result.Signals
	if signals == nil {
		signals = []string{}
	}
	common.ApiSuccess(c, gin.H{
		"tier":             result.Tier.String(),
		"score":            result.Score,
		"cause":            result.Cause,
		"signals":          signals,
		"dimensions":       dimensions,
		"estimated_tokens": result.EstimatedTokens,
	})
}
