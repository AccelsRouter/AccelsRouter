package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Model identity verification: proof that a channel really serves the model
// it claims. A fixed canary request is sent through a channel and its
// response is compared with a baseline captured from a trusted channel
// (ModelFingerprint). Every verification keeps its evidence (the exact
// request and response) so a mismatch can be handed to the upstream as-is.

// ModelFingerprint is the per-model baseline: what the canary looks like
// when answered by the genuine model, captured from the admin-chosen
// source channel.
type ModelFingerprint struct {
	Id              int    `json:"id" gorm:"primarykey"`
	ModelName       string `json:"model_name" gorm:"type:varchar(191);uniqueIndex"`
	SourceChannelId int    `json:"source_channel_id"`
	PromptTokens    int    `json:"prompt_tokens"`                           // canary input tokens the genuine model reports
	ResponseModel   string `json:"response_model" gorm:"type:varchar(191)"` // model name the genuine upstream echoes back
	IdPrefix        string `json:"id_prefix" gorm:"type:varchar(32)"`       // response id prefix (msg_, chatcmpl-)
	Evidence        string `json:"evidence,omitempty" gorm:"type:text"`     // JSON: request + response of the baseline probe
	UpdatedTime     int64  `json:"updated_time"`
}

// UpstreamIdentityResult is the latest verification of one channel × model.
type UpstreamIdentityResult struct {
	Id          int    `json:"id" gorm:"primarykey"`
	ChannelId   int    `json:"channel_id" gorm:"uniqueIndex:uq_upstream_identity,priority:1"`
	ModelName   string `json:"model_name" gorm:"type:varchar(191);uniqueIndex:uq_upstream_identity,priority:2"`
	Verdict     string `json:"verdict" gorm:"type:varchar(16)"` // verified | suspect | unverified | error
	Checks      string `json:"checks" gorm:"type:text"`         // JSON array of checks
	Evidence    string `json:"evidence,omitempty" gorm:"type:text"`
	LatencyMs   int64  `json:"latency_ms"`
	CreatedTime int64  `json:"created_time"`
}

const (
	IdentityVerdictVerified   = "verified"
	IdentityVerdictSuspect    = "suspect"
	IdentityVerdictUnverified = "unverified" // probe answered but there is no baseline to compare against
	IdentityVerdictError      = "error"
)

func GetModelFingerprint(modelName string) (*ModelFingerprint, error) {
	var fp ModelFingerprint
	err := DB.Where("model_name = ?", modelName).Limit(1).Find(&fp).Error
	if err != nil {
		return nil, err
	}
	if fp.Id == 0 {
		return nil, nil
	}
	return &fp, nil
}

// SaveModelFingerprint replaces the baseline for a model.
func SaveModelFingerprint(fp *ModelFingerprint) error {
	fp.UpdatedTime = common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing ModelFingerprint
		if err := tx.Where("model_name = ?", fp.ModelName).Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if existing.Id == 0 {
			return tx.Create(fp).Error
		}
		fp.Id = existing.Id
		return tx.Model(&ModelFingerprint{}).Where("id = ?", existing.Id).Select("*").Omit("id").Updates(fp).Error
	})
}

func ListModelFingerprints() ([]ModelFingerprint, error) {
	rows := make([]ModelFingerprint, 0)
	err := DB.Select("id", "model_name", "source_channel_id", "prompt_tokens", "response_model", "id_prefix", "updated_time").
		Order("model_name asc").Find(&rows).Error
	return rows, err
}

// SaveUpstreamIdentityResult keeps only the latest verification per channel × model.
func SaveUpstreamIdentityResult(r *UpstreamIdentityResult) error {
	r.CreatedTime = common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing UpstreamIdentityResult
		if err := tx.Where("channel_id = ? AND model_name = ?", r.ChannelId, r.ModelName).Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if existing.Id == 0 {
			return tx.Create(r).Error
		}
		r.Id = existing.Id
		return tx.Model(&UpstreamIdentityResult{}).Where("id = ?", existing.Id).Select("*").Omit("id").Updates(r).Error
	})
}

// ListUpstreamIdentityResults returns every latest verification without its
// evidence (the monitor page only needs verdicts; evidence is fetched per row).
func ListUpstreamIdentityResults() ([]UpstreamIdentityResult, error) {
	rows := make([]UpstreamIdentityResult, 0)
	err := DB.Select("id", "channel_id", "model_name", "verdict", "checks", "latency_ms", "created_time").
		Order("channel_id asc, model_name asc").Find(&rows).Error
	return rows, err
}

func GetUpstreamIdentityResult(channelId int, modelName string) (*UpstreamIdentityResult, error) {
	var r UpstreamIdentityResult
	err := DB.Where("channel_id = ? AND model_name = ?", channelId, modelName).Limit(1).Find(&r).Error
	if err != nil {
		return nil, err
	}
	if r.Id == 0 {
		return nil, nil
	}
	return &r, nil
}
