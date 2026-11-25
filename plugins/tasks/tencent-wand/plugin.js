// Fork: Tencent's WAND Vega video models, served on the OpenAI video protocol
// (/v1/videos) in the request format of the New API gateway that resells them:
// a prompt or a Responses-style message array in `input`, with `resolution`,
// `duration` and `ratio`.
//
// That gateway is the only upstream this plugin is written against. Tencent's
// own API for these models is not known here, so the driver builds the
// gateway's /v1/videos addresses for every channel and no vendor path.
//
// The gateway meters each task in the usage facts declared below and its query
// reply reports none of them, so the facts come from the request alone: the
// plugin sends the resolution and the duration it bills, defaults included,
// and forwards only the fields the gateway documents.

// Billing tokens per second of output video, by output resolution.
const MODELS = {
  "wand-vega-video-1.0-lite": {
    defaultResolution: "768P",
    tokensPerSecond: { "480P": 29700, "768P": 37500, "1080P": 50000, "2K": 62500, "4K": 75000 },
    // Input images past the free ones are billed, each as IMAGE_TOKENS.
    freeImages: 5,
  },
  // Input images are not billed.
  "wand-vega-video-1.5-lite": {
    defaultResolution: "720P",
    tokensPerSecond: { "480P": 27000, "720P": 54000, "1080P": 108000, "2K": 129600, "4K": 155500 },
  },
};
const IMAGE_TOKENS = 15000;
const MIN_DURATION = 4;
const MAX_DURATION = 15;
const DEFAULT_DURATION = 5;
// The gateway documents no limit on input images; this one bounds the tokens
// and the image count a request can be billed.
const MAX_INPUT_IMAGES = 32;

// tokens: output video tokens plus the tokens of the billed input images.
const TOKENS_FIELD = { type: "number", unit: "token", description: { en: "Token unit price", zh: "Token 单价" } };
// seconds: the requested output duration.
const SECONDS_FIELD = { type: "number", unit: "second", description: { en: "Output video unit price", zh: "输出视频单价" } };
// input_video_seconds: always 0, because video input is refused.
const INPUT_VIDEO_SECONDS_FIELD = { type: "number", unit: "second", description: { en: "Input video unit price", zh: "输入视频单价" } };
// images: the input images past the free ones.
const IMAGES_FIELD = { type: "number", unit: "count", description: { en: "Input image unit price", zh: "输入图片单价" } };

function resolutionField(resolutions) {
  return { enum: resolutions, description: { en: "Output video resolution", zh: "输出视频分辨率" } };
}

const VEGA_1_0_USAGE_SCHEMA = {
  tokens: TOKENS_FIELD,
  seconds: SECONDS_FIELD,
  input_video_seconds: INPUT_VIDEO_SECONDS_FIELD,
  images: IMAGES_FIELD,
  resolution: resolutionField(Object.keys(MODELS["wand-vega-video-1.0-lite"].tokensPerSecond)),
};

const VEGA_1_5_USAGE_SCHEMA = {
  tokens: TOKENS_FIELD,
  seconds: SECONDS_FIELD,
  input_video_seconds: INPUT_VIDEO_SECONDS_FIELD,
  resolution: resolutionField(Object.keys(MODELS["wand-vega-video-1.5-lite"].tokensPerSecond)),
};

export const meta = {
  apiVersion: 1,
  key: "tencent-wand",
  name: "Tencent WAND Vega",
  description: {
    en: "Tencent WAND Vega video generation (text-to-video and image-to-video) through a New API gateway",
    zh: "通过 New API 网关调用腾讯 WAND Vega 视频生成（文生视频、图生视频）",
  },
  version: "1.0.0",
  author: { name: "WSPN" },
  models: Object.keys(MODELS),
  fetchMode: "per_task",
  // The upstream gateway serves /v1/videos as a host protocol.
  upstreams: ["vendor", "new_api"],
  // Fallback for a channel alias that resolves to both models: every field and
  // resolution of either.
  usageSchema: Object.assign({}, VEGA_1_0_USAGE_SCHEMA, { resolution: resolutionField(["480P", "720P", "768P", "1080P", "2K", "4K"]) }),
  usageExamples: [
    { label: "1.0-lite 768P · 5s", facts: { tokens: 187500, seconds: 5, input_video_seconds: 0, images: 0, resolution: "768P" } },
    { label: "1.5-lite 720P · 5s", facts: { tokens: 270000, seconds: 5, input_video_seconds: 0, images: 0, resolution: "720P" } },
  ],
  usageProfiles: [
    {
      models: ["wand-vega-video-1.0-lite"],
      schema: VEGA_1_0_USAGE_SCHEMA,
      examples: [
        { label: "480P · 5s", facts: { tokens: 148500, seconds: 5, input_video_seconds: 0, images: 0, resolution: "480P" } },
        { label: "480P · 5s · 6 images", facts: { tokens: 163500, seconds: 5, input_video_seconds: 0, images: 1, resolution: "480P" } },
        { label: "768P · 5s", facts: { tokens: 187500, seconds: 5, input_video_seconds: 0, images: 0, resolution: "768P" } },
        { label: "1080P · 5s", facts: { tokens: 250000, seconds: 5, input_video_seconds: 0, images: 0, resolution: "1080P" } },
      ],
    },
    {
      models: ["wand-vega-video-1.5-lite"],
      schema: VEGA_1_5_USAGE_SCHEMA,
      examples: [
        { label: "480P · 5s", facts: { tokens: 135000, seconds: 5, input_video_seconds: 0, resolution: "480P" } },
        { label: "720P · 5s", facts: { tokens: 270000, seconds: 5, input_video_seconds: 0, resolution: "720P" } },
        { label: "1080P · 5s", facts: { tokens: 540000, seconds: 5, input_video_seconds: 0, resolution: "1080P" } },
        { label: "4K · 5s", facts: { tokens: 777500, seconds: 5, input_video_seconds: 0, resolution: "4K" } },
      ],
    },
  ],
  protocols: ["openai_video"],
};

function plainObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : null;
}

// The number of input images of `input`: a prompt, or messages whose content
// is a text or a list of input_text and input_image items. Any other item,
// a video among them, is refused, so no request carries an input the gateway
// would bill and this plugin would not.
function inputImageCount(input) {
  if (typeof input === "string") {
    if (!input.trim()) throw new Error("input is required");
    return 0;
  }
  if (!Array.isArray(input) || !input.length) throw new Error("input must be a prompt or an array of messages");
  let images = 0;
  for (const message of input) {
    const content = plainObject(message) ? message.content : undefined;
    if (typeof content === "string") continue;
    if (!Array.isArray(content)) throw new Error("each input message needs a content text or array");
    for (const item of content) {
      const type = plainObject(item) ? item.type : undefined;
      if (type === "input_text" || type === "text") continue;
      if (type !== "input_image" && type !== "image_url") {
        throw new Error("input content type " + JSON.stringify(type === undefined ? null : type) + " is not supported; use input_text and input_image");
      }
      const image = plainObject(item.image_url) ? item.image_url.url : item.image_url;
      if (typeof image !== "string" || !image.trim()) throw new Error("input_image needs an image_url");
      images += 1;
    }
  }
  if (images > MAX_INPUT_IMAGES) throw new Error("input accepts at most " + MAX_INPUT_IMAGES + " images");
  return images;
}

// The request sent upstream and the usage it is billed for. Rates are those of
// the model the channel sends; a channel may map a declared model to a name
// outside the table, which is then billed as the model the client asked for.
function vegaRequest(ctx) {
  const req = plainObject(ctx.requestBody) || {};
  const name = ctx.upstreamModel || ctx.model;
  const model = MODELS[name] || MODELS[ctx.model];
  if (!model) throw new Error("model " + name + " is not supported");

  let duration = DEFAULT_DURATION;
  if (req.duration !== undefined && req.duration !== null) {
    duration = req.duration;
    if (!Number.isInteger(duration) || duration < MIN_DURATION || duration > MAX_DURATION) {
      throw new Error("duration must be an integer from " + MIN_DURATION + " to " + MAX_DURATION);
    }
  }

  const resolutions = Object.keys(model.tokensPerSecond);
  let resolution = model.defaultResolution;
  if (req.resolution !== undefined && req.resolution !== null) {
    resolution = String(req.resolution).trim().toUpperCase();
    if (!resolutions.includes(resolution)) throw new Error("resolution must be one of " + resolutions.join(", "));
  }

  const images = inputImageCount(req.input);
  const facts = { tokens: duration * model.tokensPerSecond[resolution], seconds: duration, input_video_seconds: 0, resolution: resolution };
  if (model.freeImages !== undefined) {
    facts.images = Math.max(0, images - model.freeImages);
    facts.tokens += facts.images * IMAGE_TOKENS;
  }

  const body = { model: name, input: req.input, resolution: resolution, duration: duration };
  if (req.ratio !== undefined && req.ratio !== null) body.ratio = req.ratio;
  return { body: body, facts: facts, action: images ? "image_to_video" : "text_to_video" };
}

export function buildSubmitRequest(ctx) {
  const request = vegaRequest(ctx);
  return {
    url: ctx.baseUrl + "/v1/videos",
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: "Bearer " + ctx.apiKey },
    body: request.body,
    action: request.action,
  };
}

export function parseSubmitResponse(ctx, resp) {
  const body = plainObject(resp.body) || {};
  if (!body.id) throw new Error("the upstream reply has no task id");
  return { taskId: String(body.id), taskData: body };
}

// A per-call price is not multiplied by these quantities.
export function extractUsage(ctx) {
  if (ctx.usagePurpose === "billing_ratios") return null;
  return vegaRequest(ctx).facts;
}

export function buildQueryRequest(ctx) {
  return { url: ctx.baseUrl + "/v1/videos/" + encodeURIComponent(ctx.taskId), method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}

export function parseTaskResult(ctx, body) {
  const reply = plainObject(body) || {};
  const status = { queued: "QUEUED", in_progress: "IN_PROGRESS", completed: "SUCCESS", failed: "FAILURE" }[reply.status];
  if (!status) return { status: "UNKNOWN", reason: "unrecognized status: " + String(reply.status || "") };
  const result = { status: status };
  if (reply.progress > 0 && reply.progress < 100) result.progress = reply.progress + "%";
  if (status === "FAILURE") result.reason = (plainObject(reply.error) && reply.error.message) || "task failed";
  return result;
}

export function listArtifacts(task) {
  return task.status === "SUCCESS" ? [{ key: "video", type: "video" }] : [];
}

export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== "video") throw new Error("artifact_not_found");
  return {
    url: ctx.baseUrl + "/v1/videos/" + encodeURIComponent(ctx.upstreamTaskId) + "/content",
    method: ctx.clientRequest.method,
    headers: { Authorization: "Bearer " + ctx.apiKey },
  };
}

export const protocols = {
  openai_video: {
    // Accepts every JSON object: the request is checked when the upstream
    // request is built, on the channel that executes it, so a refusal answers
    // the client instead of leaving the request to another plugin.
    decodeRequest: function (ctx) {
      if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
      if (!plainObject(ctx.body.value)) throw new Error("JSON object required");
      return { kind: "submit", model: ctx.model, requestBody: Object.assign({}, ctx.body.value, { model: ctx.model }) };
    },
    // The host sets id, object, model, status, progress and the timestamps.
    render: function (ctx, task) {
      return plainObject(task.data) || {};
    },
  },
};
