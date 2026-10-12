package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// Model identity verification (see model/upstream_identity.go). A fixed canary
// is sent through a channel exactly like a channel test, and the answer is
// checked against what the genuine model is known to do: the input-token
// count its tokenizer reports for the canary (the baseline), the model name it
// echoes, and the response-id shape of its API family. Everything the probe
// sent and received is stored as evidence.

// identityCanaryPrompt is deliberately mixed (English, Chinese, code, digits,
// rare punctuation) so tokenizers of different families count it differently.
const identityCanaryPrompt = "Reply with exactly the single word OK and nothing else.\n" +
	"参考文本：量子纠缠不是超距作用。Référence: naïve façade — 42,195.00 µs; " +
	"def f(x): return x**2 // 7  |  ~!@#$%^&*()_+[]{}\\|;':\",./<>?  |  😀🚀  |  ︙｜〒。"

const identityCanaryMaxTokens = 8

// identityPromptTokenTolerance: the same text through the same tokenizer is
// exact, but channels may inject a short system prompt; allow a small slack.
const identityPromptTokenTolerance = 3

type identityCheck struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Status   string `json:"status"` // pass | fail | unknown
	Note     string `json:"note,omitempty"`
}

type identityEvidence struct {
	ChannelId     int    `json:"channel_id"`
	Model         string `json:"model"`
	UpstreamModel string `json:"upstream_model"`
	Request       string `json:"request"`
	Response      string `json:"response"`
	ProbedAt      int64  `json:"probed_at"`
	Error         string `json:"error,omitempty"`
}

// identityFamily groups channel types by the API whose response shape the
// genuine model must exhibit.
func identityFamily(channelType int) string {
	switch channelType {
	case constant.ChannelTypeAnthropic, constant.ChannelTypeAws, constant.ChannelTypeVertexAi:
		return "claude"
	case constant.ChannelTypeOpenAI, constant.ChannelTypeAzure:
		return "openai"
	case constant.ChannelTypeGemini:
		return "gemini"
	default:
		return "generic"
	}
}

var identityIdPrefix = map[string]string{"claude": "msg_", "openai": "chatcmpl-"}

// evaluateIdentity turns a canary answer into checks and a verdict. `family`
// is the channel's API family, `upstreamModel` the name sent upstream after
// mapping, `baseline` the trusted fingerprint (nil = none captured yet).
func evaluateIdentity(family, upstreamModel string, resp *dto.OpenAITextResponse, baseline *model.ModelFingerprint) ([]identityCheck, string) {
	checks := make([]identityCheck, 0, 4)
	failed := false
	unknown := false

	// 1. The model name the upstream echoes back must be the one asked for
	// (providers append dates/versions, so a prefix match is accepted).
	mc := identityCheck{Name: "response_model", Expected: upstreamModel, Actual: resp.Model}
	switch {
	case resp.Model == "":
		mc.Status, mc.Note = "unknown", "upstream did not echo a model name"
		unknown = true
	case resp.Model == upstreamModel || strings.HasPrefix(resp.Model, upstreamModel) || strings.HasPrefix(upstreamModel, resp.Model):
		mc.Status = "pass"
	default:
		mc.Status, mc.Note = "fail", "a different model answered"
		failed = true
	}
	checks = append(checks, mc)

	// 2. Response id shape of the API family (msg_ for Anthropic, chatcmpl- for
	// OpenAI). A proxy that swaps families rarely bothers to forge this.
	if prefix, ok := identityIdPrefix[family]; ok {
		ic := identityCheck{Name: "response_id", Expected: prefix + "…", Actual: resp.Id}
		if strings.HasPrefix(resp.Id, prefix) {
			ic.Status = "pass"
		} else {
			ic.Status, ic.Note = "fail", "response id is not in the family's format"
			failed = true
		}
		checks = append(checks, ic)
	}

	// 3. Tokenizer fingerprint: the canary's input-token count as reported by
	// the genuine model. Different model families count the same text
	// differently; a proxy cannot fake this without re-implementing the
	// provider's tokenizer.
	tc := identityCheck{Name: "prompt_tokens", Actual: strconv.Itoa(resp.PromptTokens)}
	if baseline == nil {
		tc.Status, tc.Expected, tc.Note = "unknown", "-", "no baseline captured for this model yet"
		unknown = true
	} else {
		tc.Expected = strconv.Itoa(baseline.PromptTokens)
		diff := resp.PromptTokens - baseline.PromptTokens
		if diff < 0 {
			diff = -diff
		}
		if diff <= identityPromptTokenTolerance {
			tc.Status = "pass"
		} else {
			tc.Status, tc.Note = "fail", fmt.Sprintf("off by %d tokens from the baseline (channel #%d)", diff, baseline.SourceChannelId)
			failed = true
		}
	}
	checks = append(checks, tc)

	// 4. Informational: where the usage came from. A locally estimated count
	// is not evidence either way.
	if resp.UsageSource != "" {
		checks = append(checks, identityCheck{Name: "usage_source", Expected: "upstream", Actual: resp.UsageSource, Status: "pass"})
	}

	switch {
	case failed:
		return checks, model.IdentityVerdictSuspect
	case unknown:
		return checks, model.IdentityVerdictUnverified
	default:
		return checks, model.IdentityVerdictVerified
	}
}

type identityProbeOutcome struct {
	Result   testResult
	Response *dto.OpenAITextResponse
	Latency  int64
}

// runIdentityProbe sends the canary through the channel (non-stream, so the
// whole answer is one JSON document) and parses it.
func runIdentityProbe(ctx context.Context, channel *model.Channel, modelName string) (identityProbeOutcome, error) {
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return identityProbeOutcome{}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	tik := time.Now()
	result := runChannelTestWithCanary(probeCtx, channel, testUserID, modelName, "", false,
		&channelTestCanary{Prompt: identityCanaryPrompt, MaxTokens: identityCanaryMaxTokens})
	out := identityProbeOutcome{Result: result, Latency: time.Since(tik).Milliseconds()}
	if result.localErr != nil {
		return out, result.localErr
	}
	var resp dto.OpenAITextResponse
	if err := common.Unmarshal(result.responseBody, &resp); err != nil {
		return out, fmt.Errorf("answer is not a chat completion: %w", err)
	}
	if result.usage != nil && resp.PromptTokens == 0 {
		resp.Usage = *result.usage
	}
	out.Response = &resp
	return out, nil
}

type identityRequest struct {
	ChannelId int    `json:"channel_id"`
	Model     string `json:"model"`
}

func identityTarget(c *gin.Context) (*model.Channel, string, bool) {
	var req identityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return nil, "", false
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.ChannelId <= 0 || req.Model == "" {
		common.ApiErrorMsg(c, "channel_id and model are required")
		return nil, "", false
	}
	channel, err := model.GetChannelById(req.ChannelId, true)
	if err != nil || channel == nil {
		common.ApiErrorMsg(c, "channel not found")
		return nil, "", false
	}
	return channel, req.Model, true
}

func identityEvidenceJSON(channel *model.Channel, modelName string, out identityProbeOutcome, probeErr error) string {
	ev := identityEvidence{
		ChannelId: channel.Id, Model: modelName, UpstreamModel: out.Result.upstreamModel,
		Request: string(out.Result.requestBody), Response: string(out.Result.responseBody),
		ProbedAt: common.GetTimestamp(),
	}
	if probeErr != nil {
		ev.Error = probeErr.Error()
	}
	return common.GetJsonString(ev)
}

// AdminUpstreamIdentityProbe — POST /api/admin/upstream/identity/probe
// Verifies one channel × model now and stores the result with its evidence.
func AdminUpstreamIdentityProbe(c *gin.Context) {
	channel, modelName, ok := identityTarget(c)
	if !ok {
		return
	}
	out, probeErr := runIdentityProbe(c.Request.Context(), channel, modelName)
	res := &model.UpstreamIdentityResult{
		ChannelId: channel.Id, ModelName: modelName, LatencyMs: out.Latency,
		Evidence: identityEvidenceJSON(channel, modelName, out, probeErr),
	}
	if probeErr != nil {
		res.Verdict = model.IdentityVerdictError
		res.Checks = common.GetJsonString([]identityCheck{{Name: "probe", Expected: "an answer", Actual: probeErr.Error(), Status: "fail"}})
	} else {
		baseline, _ := model.GetModelFingerprint(modelName)
		checks, verdict := evaluateIdentity(identityFamily(channel.Type), out.Result.upstreamModel, out.Response, baseline)
		res.Verdict = verdict
		res.Checks = common.GetJsonString(checks)
	}
	if err := model.SaveUpstreamIdentityResult(res); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, res)
}

// AdminUpstreamIdentityBaseline — POST /api/admin/upstream/identity/baseline
// Captures the fingerprint of a model from a channel the admin trusts (a
// direct official account). Later probes of every channel compare against it.
func AdminUpstreamIdentityBaseline(c *gin.Context) {
	channel, modelName, ok := identityTarget(c)
	if !ok {
		return
	}
	out, probeErr := runIdentityProbe(c.Request.Context(), channel, modelName)
	if probeErr != nil {
		common.ApiErrorMsg(c, "baseline probe failed: "+probeErr.Error())
		return
	}
	if out.Response.PromptTokens <= 0 {
		common.ApiErrorMsg(c, "the channel reported no input token count; it cannot serve as a baseline")
		return
	}
	prefix := ""
	if i := strings.IndexAny(out.Response.Id, "_-"); i > 0 {
		prefix = out.Response.Id[:i+1]
	}
	fp := &model.ModelFingerprint{
		ModelName: modelName, SourceChannelId: channel.Id,
		PromptTokens: out.Response.PromptTokens, ResponseModel: out.Response.Model, IdPrefix: prefix,
		Evidence: identityEvidenceJSON(channel, modelName, out, nil),
	}
	if err := model.SaveModelFingerprint(fp); err != nil {
		common.ApiError(c, err)
		return
	}
	// The source channel is, by definition, verified against its own baseline.
	checks, verdict := evaluateIdentity(identityFamily(channel.Type), out.Result.upstreamModel, out.Response, fp)
	_ = model.SaveUpstreamIdentityResult(&model.UpstreamIdentityResult{
		ChannelId: channel.Id, ModelName: modelName, Verdict: verdict,
		Checks: common.GetJsonString(checks), Evidence: fp.Evidence, LatencyMs: out.Latency,
	})
	common.ApiSuccess(c, fp)
}

// AdminUpstreamIdentityList — GET /api/admin/upstream/identity
// Every latest verdict plus every baseline, for the monitor page.
func AdminUpstreamIdentityList(c *gin.Context) {
	results, err := model.ListUpstreamIdentityResults()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	fingerprints, err := model.ListModelFingerprints()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"results": results, "fingerprints": fingerprints})
}

// AdminUpstreamIdentityResult — GET /api/admin/upstream/identity/result?channel_id=&model=
// One verification with its evidence.
func AdminUpstreamIdentityResult(c *gin.Context) {
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	modelName := strings.TrimSpace(c.Query("model"))
	if channelId <= 0 || modelName == "" {
		common.ApiErrorMsg(c, "channel_id and model are required")
		return
	}
	res, err := model.GetUpstreamIdentityResult(channelId, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if res == nil {
		common.ApiError(c, errors.New("not verified yet"))
		return
	}
	common.ApiSuccess(c, res)
}
