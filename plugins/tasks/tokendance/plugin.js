// TokenDance (tokendance.space) is a multi-vendor gateway with its own path
// layout: Seedance lives at /ark/v3/generations/tasks under the gateway base
// URL and speaks the Volcengine Ark wire format. The vendor plugins cannot
// reach it (they post to the vendor-native paths), so this plugin carries the
// TokenDance addresses. The channel is a New API channel bound to this plugin
// so its OpenAI-compatible text models and its video models share one key;
// the gateway is TokenDance itself in either case, so the URLs never take a
// New API route prefix.
const VIDEO_MODELS = {
  "seedance-2.0": { resolutions: ["480p", "720p", "1080p", "4k"], maxDuration: 15 },
  "seedance-2.0-fast": { resolutions: ["480p", "720p"], maxDuration: 15 },
  "seedance-2.0-mini": { resolutions: ["480p", "720p"], maxDuration: 15 },
  "seedance-2.5": { resolutions: ["480p", "720p", "1080p"], maxDuration: 30 },
};
// Ark's documented per-image ceiling for reference images, applied to uploads.
const MAX_INPUT_IMAGE_BYTES = 31457280;

function trimmed(value) {
  return String(value || "").trim();
}

// Spellings the platform exposes alongside TokenDance's dotted model IDs:
// the platform's dashed names and Volcengine's official endpoint names (also
// declared by the doubao plugin — a shared model executes on whichever plugin
// the selected channel binds). The gateway only ever receives the dotted ID.
const MODEL_ALIASES = {
  "seedance-2-0": "seedance-2.0",
  "seedance-2-0-fast": "seedance-2.0-fast",
  "seedance-2-0-mini": "seedance-2.0-mini",
  "seedance-2-5": "seedance-2.5",
  "doubao-seedance-2-0-260128": "seedance-2.0",
  "doubao-seedance-2-0-fast-260128": "seedance-2.0-fast",
  "doubao-seedance-2-0-mini-260615": "seedance-2.0-mini",
  "doubao-seedance-2-5-260628": "seedance-2.5",
};
const SEEDANCE_RESOLUTIONS = ["480p", "720p", "1080p", "4k"];
const DEFAULT_VIDEO_PROFILE = { resolutions: SEEDANCE_RESOLUTIONS, maxDuration: 30 };

// Max-pixel 16:9 dimensions per resolution tier, for the submit-time token
// estimate (Ark formula: tokens = seconds × width × height × 24 / 1024);
// completion overlays the tokens the gateway reports.
function resolutionMaxPixels(resolution) {
  if (resolution === "480p") return [854, 480];
  if (resolution === "1080p") return [1920, 1080];
  if (resolution === "4k") return [3840, 2160];
  return [1280, 720];
}

function estimateTokens(seconds, resolution) {
  const dims = resolutionMaxPixels(resolution);
  return (seconds * dims[0] * dims[1] * 24) / 1024;
}

function seedanceUsageSchema(profile) {
  const resolutionLabels = {};
  for (const resolution of profile.resolutions) resolutionLabels[resolution] = { en: resolution, zh: resolution };
  return {
    // Gateway billing tokens: estimated from duration and resolution at submit, actual on completion.
    tokens: {
      type: "number",
      unit: "token",
      description: { en: "Billing token unit price", zh: "计费 Token 单价" },
    },
    // Output video resolution; TokenDance prices Seedance tokens per resolution tier.
    resolution: {
      enum: profile.resolutions,
      enumLabels: resolutionLabels,
      description: { en: "Output video resolution", zh: "输出视频分辨率" },
    },
    // Reference video input is priced differently from image or text input.
    video_input: {
      enum: ["none", "video"],
      enumLabels: { none: { en: "No reference video", zh: "无参考视频" }, video: { en: "With reference video", zh: "有参考视频" } },
      description: { en: "Reference video input", zh: "参考视频输入" },
    },
  };
}

function seedanceUsageExamples(profile) {
  const examples = profile.resolutions.map((resolution) => ({
    label: resolution + " · 5s",
    facts: { tokens: Math.round(estimateTokens(5, resolution)), resolution: resolution, video_input: "none" },
  }));
  examples.push({ label: "720p · 5s (+reference video)", facts: { tokens: Math.round(estimateTokens(5, "720p")), resolution: "720p", video_input: "video" } });
  return examples;
}

function tokendanceModelId(name) {
  const key = trimmed(name);
  return Object.prototype.hasOwnProperty.call(MODEL_ALIASES, key) ? MODEL_ALIASES[key] : key;
}

function declaredModels() {
  return Object.keys(VIDEO_MODELS).concat(Object.keys(MODEL_ALIASES));
}

function usageProfiles() {
  const profiles = [];
  for (const model of declaredModels()) {
    const profile = VIDEO_MODELS[tokendanceModelId(model)];
    const schema = seedanceUsageSchema(profile);
    const examples = seedanceUsageExamples(profile);
    const shape = JSON.stringify([schema, examples]);
    let entry = profiles.find((item) => item.shape === shape);
    if (!entry) {
      entry = { shape: shape, models: [], schema: schema, examples: examples };
      profiles.push(entry);
    }
    entry.models.push(model);
  }
  return profiles.map((entry) => ({ models: entry.models, schema: entry.schema, examples: entry.examples }));
}

export const meta = {
  apiVersion: 1,
  key: "tokendance",
  name: "TokenDance",
  icon: "text:TD",
  description: {
    en: "Seedance video generation through the TokenDance gateway",
    zh: "通过词元跳动（TokenDance）网关生成 Seedance 视频",
  },
  version: "1.2.1",
  author: { name: "AccelsRouter" },
  website: "https://tokendance.space/docs/protocol-seedance-generations",
  baseUrl: "https://tokendance.space/gateway",
  models: declaredModels(),
  fetchMode: "per_task",
  // Bindable to a New API channel; the gateway behind it is TokenDance.
  upstreams: ["vendor", "new_api"],
  // duration -1 lets the model choose the length; the reservation covers the longest one.
  requiredCapabilities: ["duration-auto@1"],
  usageSchema: seedanceUsageSchema(DEFAULT_VIDEO_PROFILE),
  usageExamples: seedanceUsageExamples(DEFAULT_VIDEO_PROFILE),
  usageProfiles: usageProfiles(),
  routes: [
    { method: "POST", path: "/tokendance/ark/v3/generations/tasks", type: "submit", decode: "createTask", render: "taskCreated" },
    { method: "GET", path: "/tokendance/ark/v3/generations/tasks/:task_id", type: "query", render: "taskStatus" },
  ],
  protocols: [{ name: "openai_responses", supports: ["stream", "sync", "background"] }, "openai_video"],
};

function tasksRoot(ctx) {
  return ctx.baseUrl + "/ark/v3/generations/tasks";
}

function headers(ctx) {
  return { "Content-Type": "application/json", Accept: "application/json", Authorization: "Bearer " + ctx.apiKey };
}

function normalizeResolution(value) {
  const raw = trimmed(value).toLowerCase();
  if (SEEDANCE_RESOLUTIONS.includes(raw)) return raw;
  const parts = raw.replace("*", "x").split("x");
  if (parts.length !== 2) return "720p";
  const max = Math.max(Number(parts[0]), Number(parts[1]));
  if (max >= 3840) return "4k";
  if (max >= 1920) return "1080p";
  if (max >= 1280) return "720p";
  return "480p";
}

function hasVideo(content) {
  return Array.isArray(content) && content.some((item) => item && (item.type === "video_url" || Object.prototype.hasOwnProperty.call(item, "video_url")));
}

// Capability profile of the executing model; a channel mapping may rename it,
// so the declared client model still describes it.
function videoProfile(ctx) {
  for (const model of [ctx && ctx.upstreamModel, ctx && ctx.model].map(tokendanceModelId)) {
    if (Object.prototype.hasOwnProperty.call(VIDEO_MODELS, model)) return VIDEO_MODELS[model];
  }
  return DEFAULT_VIDEO_PROFILE;
}

// Resolution the reservation is estimated at: the requested tier or the tier
// of a WxH size, rejecting tiers the model does not offer. A request that
// leaves it to the gateway reserves the highest tier up to 1080p.
function videoResolution(ctx) {
  const req = ctx.requestBody || {};
  const metadata = req.metadata || {};
  const profile = videoProfile(ctx);
  const raw = trimmed(metadata.resolution || req.size).toLowerCase();
  const recognized = SEEDANCE_RESOLUTIONS.includes(raw) || raw.replace("*", "x").split("x").length === 2;
  if (!recognized) return profile.resolutions.includes("1080p") ? "1080p" : profile.resolutions[profile.resolutions.length - 1];
  const resolution = normalizeResolution(raw);
  if (!profile.resolutions.includes(resolution))
    throw new Error(trimmed(ctx.upstreamModel || ctx.model) + " resolution must be one of " + profile.resolutions.join(", "));
  return resolution;
}

function responsesInput(req) {
  const texts = [],
    images = [];
  const input = req.input;
  if (typeof input === "string") texts.push(input);
  else if (Array.isArray(input)) {
    for (const item of input) {
      if (typeof item === "string") {
        texts.push(item);
        continue;
      }
      if (!item || typeof item !== "object" || Array.isArray(item)) continue;
      const content = item.content === undefined ? [item] : Array.isArray(item.content) ? item.content : [item.content];
      for (const part of content) {
        if (typeof part === "string") {
          texts.push(part);
          continue;
        }
        if (!part || typeof part !== "object" || Array.isArray(part)) continue;
        if (["input_text", "text"].includes(part.type) && typeof part.text === "string") texts.push(part.text);
        if (["input_image", "image_url"].includes(part.type)) {
          let image = part.image_url;
          if (image && typeof image === "object") image = image.url;
          if (trimmed(image)) images.push(trimmed(image));
        }
      }
    }
  }
  return {
    prompt: texts
      .filter(function (text) {
        return trimmed(text);
      })
      .join("\n"),
    images: images,
  };
}

function responsesVideoText(ctx) {
  const artifact = ctx && ctx.artifacts && ctx.artifacts.video;
  const url = trimmed(artifact && artifact.url);
  if (!url) throw new Error("video artifact is unavailable");
  const escaped = url.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return '<video controls src="' + escaped + '"></video>';
}

export const native = {
  createTask: function (ctx) {
    if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
    const body = ctx.body.value;
    if (!body || typeof body !== "object" || Array.isArray(body)) throw new Error("request body must be an object");
    const model = trimmed(body.model);
    if (!model) throw new Error("model is required");
    if (body.content !== undefined && !Array.isArray(body.content)) throw new Error("content must be an array");
    const content = Array.isArray(body.content) ? body.content : [];
    const texts = [];
    let hasReference = false;
    for (const item of content) {
      if (!item || typeof item !== "object" || Array.isArray(item)) continue;
      if (item.type === "text" && typeof item.text === "string") texts.push(item.text);
      else hasReference = true;
    }
    if (!texts.length && !hasReference) throw new Error("content is required");
    const requestBody = {
      model: model,
      prompt: texts
        .filter(function (text) {
          return trimmed(text);
        })
        .join("\n"),
      metadata: body,
    };
    const seconds = Number(body.duration);
    if (Number.isFinite(seconds) && (seconds > 0 || seconds === -1)) requestBody.seconds = seconds;
    return { kind: "submit", model: model, action: hasReference ? "image_to_video" : "text_to_video", requestBody: requestBody };
  },
  taskCreated: function (ctx, task) {
    const data = task.data && typeof task.data === "object" && !Array.isArray(task.data) ? task.data : {};
    return Object.assign({}, data, { id: task.task_id });
  },
  taskStatus: function (ctx, task) {
    if (task.data && typeof task.data === "object" && !Array.isArray(task.data)) return Object.assign({}, task.data, { id: task.task_id });
    const statusMap = { NOT_START: "queued", SUBMITTED: "queued", QUEUED: "queued", IN_PROGRESS: "running", SUCCESS: "succeeded", FAILURE: "failed" };
    const output = { id: task.task_id, status: statusMap[task.status] || "queued" };
    if (task.fail_reason) output.error = { message: task.fail_reason };
    return output;
  },
  error: function (ctx, error) {
    return { error: { code: error.code, message: error.message } };
  },
};

export function buildSubmitRequest(ctx) {
  const req = ctx.requestBody || {};
  const metadata = req.metadata || {};
  // Reject output tiers the model does not offer before any quota is reserved.
  videoResolution(ctx);
  const body = Object.assign({ model: req.model || "", content: [] }, metadata);
  const imageContent = [];
  const images = Array.isArray(req.images) ? req.images : [];
  for (const url of images) imageContent.push({ type: "image_url", image_url: { url: url }, role: "first_frame" });
  const metadataContent = Array.isArray(body.content) ? body.content : [];
  body.content = imageContent.concat(metadataContent).filter((item) => item && item.type !== "text");
  const hasReference = body.content.length > 0;
  if (trimmed(req.prompt) || !hasReference) body.content.push({ type: "text", text: req.prompt || "" });
  if (req.size && !body.resolution) body.resolution = normalizeResolution(req.size);
  const seconds = Number.parseInt(req.seconds || "", 10);
  if (seconds > 0 || seconds === -1) body.duration = seconds;
  body.model = tokendanceModelId(ctx.upstreamModel || body.model);
  return {
    url: tasksRoot(ctx),
    method: "POST",
    headers: headers(ctx),
    body: body,
    action: hasReference ? "image_to_video" : "text_to_video",
    rewriteModel: body.model,
  };
}

export function parseSubmitResponse(ctx, resp) {
  const body = resp.body || {};
  const taskId = trimmed(body.id || body.task_id || (body.data && (body.data.id || body.data.task_id)));
  if (!taskId) throw new Error("task_id is empty");
  return { taskId: taskId, taskData: body };
}

export function extractUsage(ctx) {
  const req = ctx.requestBody || {};
  const metadata = req.metadata || {};
  const profile = videoProfile(ctx);
  // A missing duration or -1 (the model picks the length) reserves the
  // longest duration the model can produce; completion settles the real one.
  let seconds = Number(req.seconds || req.duration || metadata.duration || 0);
  if (!Number.isFinite(seconds) || seconds <= 0) seconds = profile.maxDuration;
  seconds = Math.min(seconds, 3600);
  const resolution = videoResolution(ctx);
  return {
    tokens: estimateTokens(seconds, resolution),
    resolution: resolution,
    video_input: hasVideo(metadata.content) ? "video" : "none",
  };
}

export function buildQueryRequest(ctx) {
  return { url: tasksRoot(ctx) + "/" + encodeURIComponent(ctx.taskId), method: "GET", headers: headers(ctx) };
}

export function parseTaskResult(ctx, body) {
  const status = trimmed(body && body.status).toLowerCase();
  if (status === "pending" || status === "queued") return { status: "QUEUED", progress: "10%" };
  if (status === "processing" || status === "running") return { status: "IN_PROGRESS", progress: "50%" };
  if (status === "succeeded" || status === "completed") {
    const content = (body && body.content) || {};
    return { status: "SUCCESS", progress: "100%", url: trimmed(content.video_url) };
  }
  if (status === "failed" || status === "expired" || status === "cancelled") {
    const reason = body.error && body.error.message ? body.error.message : status;
    return { status: "FAILURE", progress: "100%", reason: reason };
  }
  return { status: "UNKNOWN", reason: "unrecognized status: " + status };
}

export function extractUsageOnComplete(task, taskResult, body) {
  if (!body || trimmed(body.status).toLowerCase() !== "succeeded") return {};
  const facts = {};
  const content = body.content || {};
  const usage = body.usage || {};
  let tokens = Number(usage.completion_tokens);
  if (!Number.isFinite(tokens) || tokens <= 0) tokens = Number(usage.total_tokens);
  if (Number.isFinite(tokens) && tokens > 0) facts.tokens = tokens;
  const resolution = trimmed(content.resolution || body.resolution).toLowerCase();
  if (videoProfile(task).resolutions.includes(resolution)) facts.resolution = resolution;
  return facts;
}

export function listArtifacts(task) {
  if (task.status !== "SUCCESS") return [];
  const content = ((task && task.data) || {}).content || {};
  const artifacts = [];
  if (trimmed(content.video_url)) artifacts.push({ key: "video", type: "video" });
  if (trimmed(content.last_frame_url)) artifacts.push({ key: "last_frame", type: "image", mimeType: "image/png" });
  return artifacts;
}

export function buildContentRequest(ctx) {
  const content = ((ctx && ctx.data) || {}).content || {};
  const urls = { video: content.video_url, last_frame: content.last_frame_url };
  const url = trimmed(urls[ctx.artifactKey]);
  if (!url) throw new Error("artifact_not_found");
  return { url: url, method: ctx.clientRequest.method, credentialless: true };
}

const legacyRenderers = {
  openai_video: function (task) {
    const statuses = { NOT_START: "queued", SUBMITTED: "queued", QUEUED: "queued", IN_PROGRESS: "in_progress", SUCCESS: "completed", FAILURE: "failed" };
    const output = {
      id: task.task_id,
      object: "video",
      model: (task.properties || {}).origin_model_name || "",
      status: statuses[task.status] || "unknown",
      progress: Number(String(task.progress || "0").replace("%", "")),
      created_at: Number(task.created_at || 0),
    };
    const completedAt = Number(task.finished_at || task.updated_at || 0);
    if (completedAt > 0) output.completed_at = completedAt;
    if (task.status === "FAILURE") output.error = { code: "video_generation_failed", message: "The video generation task failed." };
    return output;
  },
};

export const protocols = {
  openai_responses: {
    decodeRequest: function (ctx) {
      if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
      const req = ctx.body.value;
      if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request body must be an object");
      const model = trimmed(req.model);
      if (!model) throw new Error("model is required");
      if (req.input !== undefined && typeof req.input !== "string" && !Array.isArray(req.input)) throw new Error("input must be a string or array");
      if (req.images !== undefined && !Array.isArray(req.images)) throw new Error("images must be an array");
      if (req.metadata !== undefined && (!req.metadata || typeof req.metadata !== "object" || Array.isArray(req.metadata)))
        throw new Error("metadata must be an object");
      const input = responsesInput(req);
      const prompt = input.prompt || trimmed(req.prompt);
      if (!prompt) throw new Error("input is required");
      const images = [];
      for (const image of [req.image, req.input_reference].concat(req.images || [], input.images)) {
        if (trimmed(image) && !images.includes(trimmed(image))) images.push(trimmed(image));
      }
      const metadata = Object.assign({}, req.metadata || {});
      if (Object.prototype.hasOwnProperty.call(req, "resolution")) metadata.resolution = req.resolution;
      else if (req.size && !metadata.resolution) metadata.resolution = normalizeResolution(req.size);
      const requestBody = { model: model, prompt: prompt, metadata: metadata };
      if (images.length) requestBody.images = images;
      if (Object.prototype.hasOwnProperty.call(req, "seconds")) requestBody.seconds = req.seconds;
      else if (Object.prototype.hasOwnProperty.call(req, "duration")) requestBody.seconds = req.duration;
      if (Object.prototype.hasOwnProperty.call(req, "size")) requestBody.size = req.size;
      return { kind: "submit", model: model, action: images.length ? "image_to_video" : "text_to_video", requestBody: requestBody };
    },
    renderEvents: function (ctx, task, previousState) {
      const status = String(task.status || "UNKNOWN").toUpperCase();
      const value = Number(String(task.progress || "").replace("%", ""));
      const progress = Number.isFinite(value) && value >= 0 && value <= 100 ? value : null;
      const state = { status: status, progress: progress };
      if (status === "SUCCESS") {
        const text = responsesVideoText(ctx);
        const events = previousState && previousState.status === status ? [] : [{ type: "output", data: text }];
        return { events: events, state: state, done: true };
      }
      if (status === "FAILURE")
        return { events: [{ type: "error", code: "task_failed", message: task.fail_reason || "task failed" }], state: state, done: true };
      if (previousState && previousState.status === status && previousState.progress === progress) return { events: [], state: state, done: false };
      const event = { type: "progress", message: status.toLowerCase() };
      if (progress !== null) event.progress = progress;
      return { events: [event], state: state, done: false };
    },
    renderFinal: function (ctx, _task) {
      return {
        output: [
          {
            type: "message",
            status: "completed",
            role: "assistant",
            content: [{ type: "output_text", text: responsesVideoText(ctx), annotations: [], logprobs: [] }],
          },
        ],
        metadata: { vendor: "tokendance" },
      };
    },
  },
  openai_video: {
    decodeRequest: function (ctx) {
      if (!ctx.body || (ctx.body.kind !== "json" && ctx.body.kind !== "multipart")) throw new Error("JSON or multipart body required");
      const images = [];
      let req;
      if (ctx.body.kind === "json") {
        req = ctx.body.value;
        if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("JSON object required");
        if (req.metadata !== undefined && (!req.metadata || typeof req.metadata !== "object" || Array.isArray(req.metadata)))
          throw new Error("metadata must be an object");
      } else {
        // The OpenAI SDK uploads the first frame as the input_reference part.
        // TokenDance speaks the Ark wire format, which reads a Base64 data URL
        // in image_url.url, so the host inlines the upload at submit time.
        req = {};
        const fields = ctx.body.fields || {};
        for (const name of Object.keys(fields)) {
          if (fields[name].length > 1) throw new Error(name + " must be provided once");
          req[name] = fields[name][0];
        }
        if (req.metadata !== undefined) {
          let parsed;
          try {
            parsed = JSON.parse(req.metadata);
          } catch (e) {
            throw new Error("metadata must be a JSON object string");
          }
          if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("metadata must be a JSON object string");
          req.metadata = parsed;
        }
        for (const file of ctx.body.files || []) {
          if (file.field !== "input_reference") throw new Error("only input_reference may be uploaded; other references must be URLs inside metadata.content");
          if (images.length || trimmed(req.input_reference) || trimmed(req.image)) throw new Error("input_reference must be provided once");
          const mimeType = trimmed(file.mimeType).toLowerCase();
          images.push({
            __fileRef: file.ref,
            encoding: "dataUrl",
            mimeType: /^image\/[a-z0-9.+-]+$/.test(mimeType) ? mimeType : "image/png",
            maxBytes: MAX_INPUT_IMAGE_BYTES,
          });
        }
      }
      const seconds = req.seconds === undefined ? req.duration : req.seconds;
      if (seconds !== undefined && Number(seconds) !== -1 && (!Number.isFinite(Number(seconds)) || Number(seconds) <= 0 || Number(seconds) > 3600))
        throw new Error("seconds must be -1 or between 1 and 3600");
      const prompt = trimmed(req.prompt);
      for (const image of [req.image, req.input_reference].concat(Array.isArray(req.images) ? req.images : [])) {
        if (trimmed(image) && !images.includes(trimmed(image))) images.push(trimmed(image));
      }
      const metadata = Object.assign({}, req.metadata || {});
      if (Object.prototype.hasOwnProperty.call(req, "resolution")) metadata.resolution = req.resolution;
      else if (req.size && !metadata.resolution) metadata.resolution = normalizeResolution(req.size);
      if (Object.prototype.hasOwnProperty.call(req, "ratio")) metadata.ratio = req.ratio;
      if (!prompt && !images.length && !Array.isArray(metadata.content)) throw new Error("prompt is required");
      const requestBody = { model: ctx.model, prompt: prompt, metadata: metadata };
      if (images.length) requestBody.images = images;
      if (seconds !== undefined) requestBody.seconds = Number(seconds);
      if (Object.prototype.hasOwnProperty.call(req, "size")) requestBody.size = req.size;
      const hasReference = images.length > 0 || (Array.isArray(metadata.content) && metadata.content.some((item) => item && item.type !== "text"));
      return { kind: "submit", model: ctx.model, action: hasReference ? "image_to_video" : "text_to_video", requestBody: requestBody };
    },
    render: function (ctx, task) {
      if (task.data && typeof task.data === "object" && !Array.isArray(task.data)) return task.data;
      return legacyRenderers.openai_video(task);
    },
  },
};
